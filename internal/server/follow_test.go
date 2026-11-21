package server_test

import (
	"context"
	"errors"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Aset-Mk/geomatch/internal/client"
	"github.com/Aset-Mk/geomatch/internal/feed"
	"github.com/Aset-Mk/geomatch/internal/gateway"
	"github.com/Aset-Mk/geomatch/internal/geo"
	"github.com/Aset-Mk/geomatch/internal/index"
	"github.com/Aset-Mk/geomatch/internal/server"
	"github.com/Aset-Mk/geomatch/internal/sim"
)

const topic = "positions"

// kafka starts an in-memory Kafka and returns a producer to it
func kafka(t *testing.T) (*kgo.Client, kgo.Opt) {
	t.Helper()

	c, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(4, topic))
	if err != nil {
		t.Fatalf("NewCluster: %v", err)
	}
	t.Cleanup(c.Close)
	seeds := kgo.SeedBrokers(c.ListenAddrs()...)

	p, err := kgo.NewClient(seeds, kgo.DefaultProduceTopic(topic))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(p.Close)
	return p, seeds
}

// follow runs Follow in the background and returns a channel closed on ready
func follow(t *testing.T, m *server.Server, ttl time.Duration, seeds kgo.Opt) <-chan struct{} {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	ready := make(chan struct{})
	go func() { done <- m.Follow(ctx, topic, ttl, func() { close(ready) }, seeds) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Follow: %v", err)
		}
	})
	return ready
}

func TestGatewayToIndex(t *testing.T) {
	p, seeds := kafka(t)
	b := sim.CityBounds
	lat := (b.Min.Lat + b.Max.Lat) / 2
	local, idx := index.NewGrid(200, lat), index.NewGrid(200, lat)
	<-follow(t, server.New(idx), 10*time.Second, seeds)

	drivers := client.New(dial(t, gateway.New(p, topic)))
	r := rand.New(rand.NewPCG(1, 1))
	us := make([]index.Update, 2000)
	for i := range us {
		us[i] = index.Update{ID: index.DriverID(i), Pos: b.RandomPoint(r)}
	}
	local.UpsertBatch(us)
	drivers.UpsertBatch(us)
	local.Remove(7)
	drivers.Remove(7)
	if err := drivers.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	points := make([]geo.Point, 100)
	for i := range points {
		points[i] = b.RandomPoint(r)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		diff := ""
		for _, p := range points {
			got, want := idx.Nearest(p, 5), local.Nearest(p, 5)
			if len(got) != len(want) {
				diff = "different number of drivers"
				break
			}
			for i := range want {
				if got[i].ID != want[i].ID {
					diff = "different drivers"
				}
			}
		}
		if diff == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("index after Kafka differs from a local one: %s", diff)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestFollowRecovers(t *testing.T) {
	p, seeds := kafka(t)
	b := sim.CityBounds
	r := rand.New(rand.NewPCG(2, 2))
	pos := make([]geo.Point, 6)
	for i := range pos {
		pos[i] = b.RandomPoint(r)
	}

	now := time.Now()
	write := func(rec *kgo.Record, age time.Duration) {
		t.Helper()
		rec.Timestamp = now.Add(-age)
		if err := p.ProduceSync(context.Background(), rec).FirstErr(); err != nil {
			t.Fatalf("ProduceSync: %v", err)
		}
	}
	position := func(id index.DriverID, p geo.Point) *kgo.Record {
		t.Helper()
		rec, err := feed.Position(topic, id, p)
		if err != nil {
			t.Fatalf("Position: %v", err)
		}
		return rec
	}
	write(position(1, pos[0]), time.Hour)
	write(position(2, pos[1]), 3*time.Second)
	write(position(2, pos[2]), 2*time.Second)
	write(position(3, pos[3]), 2*time.Second)
	write(feed.Removal(topic, 3), time.Second)
	write(position(4, pos[4]), time.Second)
	write(position(4, pos[5]), 2*time.Second)

	idx := index.NewGrid(200, (b.Min.Lat+b.Max.Lat)/2)
	select {
	case <-follow(t, server.New(idx), 10*time.Second, seeds):
	case <-time.After(5 * time.Second):
		t.Fatal("Follow did not catch up")
	}

	at := func(id index.DriverID, p geo.Point) bool {
		n := idx.Nearest(p, 1)
		return len(n) == 1 && n[0].ID == id && n[0].Dist < 1
	}
	if at(1, pos[0]) {
		t.Error("driver 1 is back from a position older than the ttl")
	}
	if !at(2, pos[2]) {
		t.Error("driver 2 is not at its last position")
	}
	if at(3, pos[3]) {
		t.Error("driver 3 is in the index after its removal")
	}
	if !at(4, pos[4]) {
		t.Error("driver 4 moved to a position older than the one it had")
	}
}

type failing struct{}

func (failing) Produce(_ context.Context, r *kgo.Record, promise func(*kgo.Record, error)) {
	promise(r, errors.New("broker down"))
}

func TestGatewayReportsKafkaFailure(t *testing.T) {
	drivers := client.New(dial(t, gateway.New(failing{}, topic)))
	drivers.Upsert(1, sim.CityBounds.Min)
	err := drivers.Flush()
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Flush after Kafka refused the positions returned %v, want Unavailable", err)
	}
}
