package serve

import (
	"context"
	"net"
	"sync/atomic"

	"google.golang.org/grpc/stats"
)

// Limit closes new connections on arrival while max are open, a connection counts once its handshake is done
type Limit struct {
	max  int64
	open atomic.Int64
}

func NewLimit(max int) *Limit {
	return &Limit{max: int64(max)}
}

// Listener hands out connections unwrapped, gRPC reads only a bare TCP connection without a buffer of its own
func (l *Limit) Listener(lis net.Listener) net.Listener {
	return &limited{Listener: lis, l: l}
}

type limited struct {
	net.Listener
	l *Limit
}

func (lis *limited) Accept() (net.Conn, error) {
	for {
		c, err := lis.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if lis.l.open.Load() >= lis.l.max {
			connectionsRefused.Inc()
			_ = c.Close()
			continue
		}
		return c, nil
	}
}

// HandleConn counts connections, Limit is a stats handler so that gRPC reports when they open and close
func (l *Limit) HandleConn(_ context.Context, s stats.ConnStats) {
	switch s.(type) {
	case *stats.ConnBegin:
		l.open.Add(1)
		connections.Inc()
	case *stats.ConnEnd:
		l.open.Add(-1)
		connections.Dec()
	}
}

func (l *Limit) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context { return ctx }
func (l *Limit) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context   { return ctx }
func (l *Limit) HandleRPC(context.Context, stats.RPCStats)                         {}
