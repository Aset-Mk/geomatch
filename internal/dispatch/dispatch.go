// Package dispatch offers orders to the nearest free drivers and keeps driver states in Redis
package dispatch

import (
	"context"
	"errors"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Aset-Mk/geomatch/internal/geo"
	"github.com/Aset-Mk/geomatch/internal/index"
	pb "github.com/Aset-Mk/geomatch/internal/pb/matcherv1"
)

// grace keeps an offer in Redis a bit longer than the matcher waits, so the matcher frees the driver itself while it is alive
const grace = time.Second

// subBuffer is how many offers wait for a slow Offers stream before new ones are dropped
const subBuffer = 4096

type Config struct {
	Candidates   int           // drivers asked from the index for every offer
	MaxOffers    int           // drivers the order is offered to before it fails
	OfferTimeout time.Duration // how long a driver has to answer
}

// Dispatcher serves the Orders API, the index gives candidates and Redis decides who gets the order
type Dispatcher struct {
	pb.UnimplementedOrdersServer
	idx index.Index
	st  *Store
	cfg Config

	mu     sync.Mutex
	orders map[uint64]*order
	subs   map[chan *pb.Offer]struct{}
}

// order is waited on by its CreateOrder call, which alone changes the state of its driver
type order struct {
	answers chan answer
	done    chan struct{}
}

type answer struct {
	driver index.DriverID
	accept bool
	result chan error
}

var errNotOffered = status.Error(codes.FailedPrecondition, "the offer is not open")

func New(idx index.Index, st *Store, cfg Config) *Dispatcher {
	return &Dispatcher{idx: idx, st: st, cfg: cfg, orders: make(map[uint64]*order), subs: make(map[chan *pb.Offer]struct{})}
}

func (d *Dispatcher) CreateOrder(ctx context.Context, req *pb.CreateOrderRequest) (*pb.CreateOrderResponse, error) {
	p := geo.Point{Lat: req.GetPickup().GetLat(), Lon: req.GetPickup().GetLon()}
	if req.GetPickup() == nil || !p.Valid() {
		return nil, status.Errorf(codes.InvalidArgument, "bad pickup %v", req.GetPickup())
	}
	began := time.Now()

	id, o := d.register()
	defer d.unregister(id, o)

	tried := make([]index.DriverID, 0, d.cfg.MaxOffers)
	for range d.cfg.MaxOffers {
		cands, err := d.candidates(p, tried)
		if err != nil {
			return nil, d.failed(err)
		}
		if len(cands) == 0 {
			break
		}
		drv, ok, err := d.st.Offer(ctx, id, cands, d.cfg.OfferTimeout+grace)
		if err != nil {
			return nil, d.failed(err)
		}
		if !ok {
			break
		}
		tried = append(tried, drv)
		d.publish(&pb.Offer{OrderId: id, DriverId: uint64(drv), Pickup: req.GetPickup()})

		accepted, err := d.wait(ctx, o, id, drv)
		if err != nil {
			return nil, d.failed(err)
		}
		if accepted {
			ordersTotal.WithLabelValues("assigned").Inc()
			assignSeconds.Observe(time.Since(began).Seconds())
			return &pb.CreateOrderResponse{OrderId: id, Assigned: true, DriverId: uint64(drv)}, nil
		}
	}
	ordersTotal.WithLabelValues("no_driver").Inc()
	return &pb.CreateOrderResponse{OrderId: id}, nil
}

// wait handles answers of the driver until the offer is accepted, declined or times out
func (d *Dispatcher) wait(ctx context.Context, o *order, id uint64, drv index.DriverID) (bool, error) {
	timer := time.NewTimer(d.cfg.OfferTimeout)
	defer timer.Stop()
	for {
		select {
		case a := <-o.answers:
			if a.driver != drv {
				a.result <- errNotOffered
				continue
			}
			if a.accept {
				ok, err := d.st.Accept(ctx, id, drv)
				switch {
				case err != nil:
					a.result <- status.Error(codes.Unavailable, "redis")
					return false, err
				case !ok:
					// Redis dropped the offer by its TTL
					a.result <- errNotOffered
					offersTotal.WithLabelValues("expired").Inc()
					return false, nil
				}
				a.result <- nil
				offersTotal.WithLabelValues("accepted").Inc()
				return true, nil
			}
			_, err := d.st.Release(ctx, id, drv)
			a.result <- err
			offersTotal.WithLabelValues("declined").Inc()
			return false, err
		case <-timer.C:
			offersTotal.WithLabelValues("timeout").Inc()
			_, err := d.st.Release(ctx, id, drv)
			return false, err
		case <-ctx.Done():
			// the caller is gone, the driver must not stay offered an order nobody waits for
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			_, _ = d.st.Release(rctx, id, drv)
			cancel()
			return false, ctx.Err()
		}
	}
}

func (d *Dispatcher) AnswerOffer(ctx context.Context, req *pb.AnswerOfferRequest) (*pb.AnswerOfferResponse, error) {
	d.mu.Lock()
	o := d.orders[req.GetOrderId()]
	d.mu.Unlock()
	if o == nil {
		return nil, errNotOffered
	}
	a := answer{driver: index.DriverID(req.GetDriverId()), accept: req.GetAccept(), result: make(chan error, 1)}
	select {
	case o.answers <- a:
	case <-o.done:
		return nil, errNotOffered
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := <-a.result; err != nil {
		return nil, err
	}
	return &pb.AnswerOfferResponse{}, nil
}

func (d *Dispatcher) CompleteOrder(ctx context.Context, req *pb.CompleteOrderRequest) (*pb.CompleteOrderResponse, error) {
	ok, err := d.st.Complete(ctx, req.GetOrderId(), index.DriverID(req.GetDriverId()))
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "redis: %v", err)
	}
	if !ok {
		return nil, status.Error(codes.FailedPrecondition, "the driver is not on this order")
	}
	return &pb.CompleteOrderResponse{}, nil
}

func (d *Dispatcher) Offers(_ *pb.OffersRequest, stream grpc.ServerStreamingServer[pb.Offer]) error {
	ch := make(chan *pb.Offer, subBuffer)
	d.mu.Lock()
	d.subs[ch] = struct{}{}
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.subs, ch)
		d.mu.Unlock()
	}()
	// the client knows from the headers that offers from now on reach it
	if err := stream.SendHeader(nil); err != nil {
		return err
	}

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case off := <-ch:
			if err := stream.Send(off); err != nil {
				return err
			}
		}
	}
}

// publish sends the offer to every Offers stream, a stream that is behind loses it and the offer times out
func (d *Dispatcher) publish(off *pb.Offer) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for ch := range d.subs {
		select {
		case ch <- off:
		default:
			offersDropped.Inc()
		}
	}
}

// candidates are the nearest drivers this order was not offered to yet
func (d *Dispatcher) candidates(p geo.Point, tried []index.DriverID) ([]index.DriverID, error) {
	k := d.cfg.Candidates + len(tried)
	var found []index.Neighbor
	if r, ok := d.idx.(interface {
		Search(geo.Point, int) ([]index.Neighbor, error)
	}); ok {
		var err error
		if found, err = r.Search(p, k); err != nil {
			return nil, err
		}
	} else {
		found = d.idx.Nearest(p, k)
	}
	out := make([]index.DriverID, 0, d.cfg.Candidates)
	for _, n := range found {
		if !slices.Contains(tried, n.ID) && len(out) < d.cfg.Candidates {
			out = append(out, n.ID)
		}
	}
	return out, nil
}

func (d *Dispatcher) register() (uint64, *order) {
	o := &order{answers: make(chan answer), done: make(chan struct{})}
	d.mu.Lock()
	defer d.mu.Unlock()
	// a random id stays unique across matchers without asking anyone
	id := rand.Uint64()
	for d.orders[id] != nil {
		id = rand.Uint64()
	}
	d.orders[id] = o
	return id, o
}

func (d *Dispatcher) unregister(id uint64, o *order) {
	d.mu.Lock()
	delete(d.orders, id)
	d.mu.Unlock()
	close(o.done)
}

func (d *Dispatcher) failed(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		ordersTotal.WithLabelValues("canceled").Inc()
		return status.FromContextError(err).Err()
	}
	ordersTotal.WithLabelValues("error").Inc()
	return status.Errorf(codes.Unavailable, "redis: %v", err)
}
