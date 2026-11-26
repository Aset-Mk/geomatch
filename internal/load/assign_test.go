package load

import (
	"context"
	"math/rand/v2"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/Aset-Mk/geomatch/internal/geo"
	pb "github.com/Aset-Mk/geomatch/internal/pb/matcherv1"
)

// sameDriver gives every order to driver 7 and remembers which rides were completed
type sameDriver struct {
	pb.UnimplementedOrdersServer
	mu        sync.Mutex
	order     uint64
	completed []uint64
}

func (s *sameDriver) CreateOrder(context.Context, *pb.CreateOrderRequest) (*pb.CreateOrderResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order++
	return &pb.CreateOrderResponse{OrderId: s.order, Assigned: true, DriverId: 7}, nil
}

func (s *sameDriver) CompleteOrder(_ context.Context, req *pb.CompleteOrderRequest) (*pb.CompleteOrderResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completed = append(s.completed, req.GetOrderId())
	return &pb.CompleteOrderResponse{}, nil
}

func dialOrders(t *testing.T, s pb.OrdersServer) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	pb.RegisterOrdersServer(srv, s)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestAssignerCountsDouble(t *testing.T) {
	srv := &sameDriver{}
	conn := dialOrders(t, srv)
	a := NewAssigner([]grpc.ClientConnInterface{conn}, conn, 0, 0, 50*time.Millisecond, rand.New(rand.NewPCG(1, 1)))
	p := geo.Point{Lat: 55.75, Lon: 37.61}

	a.Order(p)
	a.Order(p)
	time.Sleep(200 * time.Millisecond)
	a.Order(p)
	st, err := a.Close()

	if st.Assigned != 3 || st.Double != 1 {
		t.Errorf("stats = %+v, want 3 assigned and 1 double, the third order came after the ride ended", st)
	}
	if err == nil {
		t.Error("Close returned no error with a double assignment")
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.completed) != 2 || srv.completed[0] != 2 || srv.completed[1] != 3 {
		t.Errorf("completed rides %v, want 2 after its ride time and 3 on Close", srv.completed)
	}
}
