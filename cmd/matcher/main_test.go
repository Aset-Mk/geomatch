package main

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	pb "github.com/Aset-Mk/geomatch/internal/pb/matcherv1"
)

// start runs the matcher on a free port and returns its address, the stop function and the result of run
func start(t *testing.T, shutdown time.Duration) (string, context.CancelFunc, <-chan error) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan error, 1)
	cfg := matcherConfig{CellSize: 200, DriverTTL: 10 * time.Second, ShutdownTimeout: shutdown, MaxConns: 2}
	go func() { done <- run(ctx, lis, cfg) }()
	return lis.Addr().String(), cancel, done
}

func TestHealth(t *testing.T) {
	addr, stop, done := start(t, time.Second)

	if err := check(addr); err != nil {
		t.Fatalf("check on a running matcher: %v", err)
	}
	stop()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := check(addr); err == nil {
		t.Error("check passed on a stopped matcher, want error")
	}
}

func TestStopDoesNotWaitForOpenStream(t *testing.T) {
	const shutdown = 200 * time.Millisecond
	addr, stop, done := start(t, shutdown)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = conn.Close() }()
	stream, err := pb.NewMatcherClient(conn).UpdateDrivers(context.Background())
	if err != nil {
		t.Fatalf("UpdateDrivers: %v", err)
	}
	if err := stream.Send(&pb.UpdateDriversRequest{}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	began := time.Now()
	stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(10 * shutdown):
		t.Fatalf("matcher still runs %v after stop with an open stream, want it to stop in %v", time.Since(began), shutdown)
	}
}

func TestConnectionLimit(t *testing.T) {
	addr, _, _ := start(t, time.Second)

	// start allows 2 connections
	held := make([]*grpc.ClientConn, 2)
	for i := range held {
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		defer func() { _ = conn.Close() }()
		if _, err := healthpb.NewHealthClient(conn).Check(context.Background(), &healthpb.HealthCheckRequest{}); err != nil {
			t.Fatalf("connection %d within the limit: %v", i+1, err)
		}
		held[i] = conn
	}

	extra, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = extra.Close() }()
	_ = extra.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := extra.Read(make([]byte, 1)); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("connection above the limit stays open, Read returned %v, want it closed by the matcher", err)
	}

	_ = held[0].Close()
	deadline := time.Now().Add(2 * time.Second)
	for check(addr) != nil {
		if time.Now().After(deadline) {
			t.Fatal("matcher takes no new connection after an old one closed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
