package load

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"

	"github.com/Aset-Mk/geomatch/internal/geo"
	pb "github.com/Aset-Mk/geomatch/internal/pb/matcherv1"
)

// Assigner places orders through the Orders API and plays the drivers who answer the offers
type Assigner struct {
	orders  []pb.OrdersClient
	next    atomic.Uint64
	drivers pb.OrdersClient
	decline float64 // share of offers declined
	ignore  float64 // share of offers left without an answer
	ride    time.Duration

	mu     sync.Mutex
	r      *rand.Rand
	onRide map[uint64]ride // drivers on a ride as seen here

	assigned, noDriver, failed, double atomic.Uint64
	err                                atomic.Pointer[error]
}

type ride struct {
	order uint64
	end   *time.Timer
}

// AssignStats counts orders by result, Double is drivers who got an order while on a ride
type AssignStats struct {
	Assigned uint64 `json:"assigned"`
	NoDriver uint64 `json:"no_driver"`
	Failed   uint64 `json:"failed"`
	Double   uint64 `json:"double"`
}

// NewAssigner places orders over orderConns in turn, the drivers answer over their own connection,
// an order waits for the answer and would hold the stream an answer needs
func NewAssigner(orderConns []grpc.ClientConnInterface, driverConn grpc.ClientConnInterface, decline, ignore float64, rideTime time.Duration, r *rand.Rand) *Assigner {
	orders := make([]pb.OrdersClient, len(orderConns))
	for i, c := range orderConns {
		orders[i] = pb.NewOrdersClient(c)
	}
	return &Assigner{orders: orders, drivers: pb.NewOrdersClient(driverConn), decline: decline, ignore: ignore, ride: rideTime, r: r, onRide: make(map[uint64]ride)}
}

// Answer reads offers until ctx is done and answers each one in its own goroutine
func (a *Assigner) Answer(ctx context.Context) error {
	stream, err := a.drivers.Offers(ctx, &pb.OffersRequest{})
	if err != nil {
		return err
	}
	// the matcher sends headers once the stream is subscribed, offers made before that are lost
	if _, err := stream.Header(); err != nil {
		return err
	}
	go func() {
		var wg sync.WaitGroup
		defer wg.Wait()
		for {
			off, err := stream.Recv()
			if err != nil {
				return
			}
			a.mu.Lock()
			x := a.r.Float64()
			a.mu.Unlock()
			if x < a.ignore {
				continue
			}
			req := &pb.AnswerOfferRequest{OrderId: off.GetOrderId(), DriverId: off.GetDriverId(), Accept: x >= a.ignore+a.decline}
			wg.Go(func() {
				// a late answer to an offer that timed out is expected
				_, _ = a.drivers.AnswerOffer(ctx, req)
			})
		}
	}()
	return nil
}

// Order places one order and, if a driver took it, ends the ride after the ride time
func (a *Assigner) Order(p geo.Point) {
	c := a.orders[a.next.Add(1)%uint64(len(a.orders))]
	resp, err := c.CreateOrder(context.Background(), &pb.CreateOrderRequest{Pickup: &pb.Point{Lat: p.Lat, Lon: p.Lon}})
	if err != nil {
		a.failed.Add(1)
		a.err.CompareAndSwap(nil, &err)
		return
	}
	if !resp.GetAssigned() {
		a.noDriver.Add(1)
		return
	}
	a.assigned.Add(1)
	drv, order := resp.GetDriverId(), resp.GetOrderId()
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, busy := a.onRide[drv]; busy {
		a.double.Add(1)
	}
	a.onRide[drv] = ride{order, time.AfterFunc(a.ride, func() { a.complete(drv, order) })}
}

// complete marks the driver free here before the matcher does, so a new order for the driver is never counted as double by mistake
func (a *Assigner) complete(drv, order uint64) {
	a.mu.Lock()
	if r, ok := a.onRide[drv]; !ok || r.order != order {
		a.mu.Unlock()
		return
	}
	delete(a.onRide, drv)
	a.mu.Unlock()
	if _, err := a.drivers.CompleteOrder(context.Background(), &pb.CompleteOrderRequest{OrderId: order, DriverId: drv}); err != nil {
		a.failed.Add(1)
		a.err.CompareAndSwap(nil, &err)
	}
}

// Close ends every ride still going and returns the counts and the first error
func (a *Assigner) Close() (AssignStats, error) {
	a.mu.Lock()
	left := make(map[uint64]uint64, len(a.onRide))
	for d, r := range a.onRide {
		r.end.Stop()
		left[d] = r.order
	}
	a.mu.Unlock()
	for d, o := range left {
		a.complete(d, o)
	}

	st := AssignStats{Assigned: a.assigned.Load(), NoDriver: a.noDriver.Load(), Failed: a.failed.Load(), Double: a.double.Load()}
	var err error
	if p := a.err.Load(); p != nil {
		err = *p
	}
	if st.Double > 0 {
		err = errors.Join(err, errors.New("a driver got an order while on a ride"))
	}
	return st, err
}
