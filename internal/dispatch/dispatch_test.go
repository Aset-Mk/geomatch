package dispatch_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/rueidis"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/Aset-Mk/geomatch/internal/dispatch"
	"github.com/Aset-Mk/geomatch/internal/geo"
	"github.com/Aset-Mk/geomatch/internal/index"
	pb "github.com/Aset-Mk/geomatch/internal/pb/matcherv1"
)

var center = geo.Point{Lat: 55.75, Lon: 37.61}

// start serves a dispatcher over an in-memory connection, drivers stand 100 m apart north of center
func start(t *testing.T, drivers int, cfg dispatch.Config) (pb.OrdersClient, *miniredis.Miniredis) {
	t.Helper()
	srv := miniredis.RunT(t)
	rc, err := rueidis.NewClient(rueidis.ClientOption{InitAddress: []string{srv.Addr()}, DisableCache: true})
	if err != nil {
		t.Fatalf("rueidis: %v", err)
	}
	t.Cleanup(rc.Close)

	idx := index.NewNaive()
	for i := range drivers {
		idx.Upsert(index.DriverID(i), geo.Offset(center, float64(i)*100, 0))
	}

	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	pb.RegisterOrdersServer(gs, dispatch.New(idx, dispatch.NewStore(rc), cfg))
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return pb.NewOrdersClient(conn), srv
}

// answer plays the drivers, decide returns whether a driver accepts and false for no answer at all
func answer(t *testing.T, c pb.OrdersClient, decide func(*pb.Offer) (accept, reply bool)) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := c.Offers(ctx, &pb.OffersRequest{})
	if err != nil {
		t.Fatalf("Offers: %v", err)
	}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})
	if _, err := stream.Header(); err != nil {
		t.Fatalf("Offers header: %v", err)
	}
	wg.Go(func() {
		for {
			off, err := stream.Recv()
			if err != nil {
				return
			}
			accept, reply := decide(off)
			if !reply {
				continue
			}
			wg.Go(func() {
				_, _ = c.AnswerOffer(ctx, &pb.AnswerOfferRequest{OrderId: off.GetOrderId(), DriverId: off.GetDriverId(), Accept: accept})
			})
		}
	})
}

func create(t *testing.T, c pb.OrdersClient) *pb.CreateOrderResponse {
	t.Helper()
	resp, err := c.CreateOrder(context.Background(), &pb.CreateOrderRequest{Pickup: &pb.Point{Lat: center.Lat, Lon: center.Lon}})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	return resp
}

func TestNoDriverGetsTwoOrders(t *testing.T) {
	const drivers, orders = 50, 2000
	c, _ := start(t, drivers, dispatch.Config{Candidates: 10, MaxOffers: 3, OfferTimeout: 5 * time.Second})
	answer(t, c, func(*pb.Offer) (bool, bool) { return true, true })

	var mu sync.Mutex
	got := make(map[uint64]int)
	assigned := 0
	var wg sync.WaitGroup
	for range orders {
		wg.Go(func() {
			resp, err := c.CreateOrder(context.Background(), &pb.CreateOrderRequest{Pickup: &pb.Point{Lat: center.Lat, Lon: center.Lon}})
			if err != nil {
				t.Errorf("CreateOrder: %v", err)
				return
			}
			if resp.GetAssigned() {
				mu.Lock()
				got[resp.GetDriverId()]++
				assigned++
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	for id, n := range got {
		if n > 1 {
			t.Errorf("driver %d got %d orders", id, n)
		}
	}
	// the first 10 drivers are the candidates of every order, the rest are never asked
	if assigned != 10 {
		t.Errorf("%d orders assigned, want 10", assigned)
	}
}

func TestOfferMovesOnAfterDeclineAndSilence(t *testing.T) {
	c, srv := start(t, 3, dispatch.Config{Candidates: 10, MaxOffers: 3, OfferTimeout: 200 * time.Millisecond})
	answer(t, c, func(off *pb.Offer) (bool, bool) {
		switch off.GetDriverId() {
		case 0:
			return false, true
		case 1:
			return false, false
		}
		return true, true
	})

	resp := create(t, c)
	if !resp.GetAssigned() || resp.GetDriverId() != 2 {
		t.Fatalf("CreateOrder = %v, want driver 2 after driver 0 declined and driver 1 kept silent", resp)
	}
	for _, key := range []string{"{city}:driver:0", "{city}:driver:1"} {
		if srv.Exists(key) {
			t.Errorf("%s is still taken after the offer ended", key)
		}
	}
}

func TestCompleteFreesDriver(t *testing.T) {
	c, _ := start(t, 1, dispatch.Config{Candidates: 10, MaxOffers: 3, OfferTimeout: time.Second})
	answer(t, c, func(*pb.Offer) (bool, bool) { return true, true })

	first := create(t, c)
	if !first.GetAssigned() {
		t.Fatal("the only driver did not get the first order")
	}
	if create(t, c).GetAssigned() {
		t.Fatal("a driver on a ride got a second order")
	}
	_, err := c.CompleteOrder(context.Background(), &pb.CompleteOrderRequest{OrderId: first.GetOrderId() + 1, DriverId: first.GetDriverId()})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("CompleteOrder of another order = %v, want FailedPrecondition", err)
	}
	if _, err := c.CompleteOrder(context.Background(), &pb.CompleteOrderRequest{OrderId: first.GetOrderId(), DriverId: first.GetDriverId()}); err != nil {
		t.Fatalf("CompleteOrder: %v", err)
	}
	if !create(t, c).GetAssigned() {
		t.Fatal("the driver is not free after CompleteOrder")
	}
}

func TestCanceledOrderFreesDriver(t *testing.T) {
	c, srv := start(t, 1, dispatch.Config{Candidates: 10, MaxOffers: 3, OfferTimeout: time.Minute})
	offered := make(chan struct{}, 1)
	answer(t, c, func(*pb.Offer) (bool, bool) {
		offered <- struct{}{}
		return false, false
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-offered
		cancel()
	}()
	_, err := c.CreateOrder(ctx, &pb.CreateOrderRequest{Pickup: &pb.Point{Lat: center.Lat, Lon: center.Lon}})
	if status.Code(err) != codes.Canceled {
		t.Fatalf("CreateOrder = %v, want Canceled", err)
	}
	// the server sees the cancel a moment after the client
	deadline := time.Now().Add(time.Second)
	for srv.Exists("{city}:driver:0") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if srv.Exists("{city}:driver:0") {
		t.Fatal("driver 0 stays offered an order nobody waits for")
	}
}
