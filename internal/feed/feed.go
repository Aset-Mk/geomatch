// Package feed turns driver positions into Kafka records and back
package feed

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"

	"github.com/Aset-Mk/geomatch/internal/geo"
	"github.com/Aset-Mk/geomatch/internal/index"
	pb "github.com/Aset-Mk/geomatch/internal/pb/matcherv1"
)

// Position is the record of a driver position, the key is the driver so that one driver stays in one partition and in order
func Position(topic string, id index.DriverID, p geo.Point) (*kgo.Record, error) {
	v, err := proto.Marshal(&pb.Point{Lat: p.Lat, Lon: p.Lon})
	if err != nil {
		return nil, err
	}
	return &kgo.Record{Topic: topic, Key: key(id), Value: v}, nil
}

// Removal is the record of a driver leaving, a record with no value
func Removal(topic string, id index.DriverID) *kgo.Record {
	return &kgo.Record{Topic: topic, Key: key(id)}
}

// Parse reads a record back, p is unset when the driver left
func Parse(r *kgo.Record) (id index.DriverID, p geo.Point, removed bool, err error) {
	if len(r.Key) != 8 {
		return 0, geo.Point{}, false, fmt.Errorf("key of %d bytes, want 8", len(r.Key))
	}
	id = index.DriverID(binary.BigEndian.Uint64(r.Key))
	if r.Value == nil {
		return id, geo.Point{}, true, nil
	}
	var pt pb.Point
	if err := proto.Unmarshal(r.Value, &pt); err != nil {
		return 0, geo.Point{}, false, err
	}
	p = geo.Point{Lat: pt.GetLat(), Lon: pt.GetLon()}
	if !p.Valid() {
		return 0, geo.Point{}, false, fmt.Errorf("bad point %v", p)
	}
	return id, p, false, nil
}

func key(id index.DriverID) []byte {
	return binary.BigEndian.AppendUint64(nil, uint64(id))
}

// EnsureTopic creates the topic unless it exists and retries until Kafka answers or ctx is done
// Positions older than an hour are of no use, so the topic keeps an hour
func EnsureTopic(ctx context.Context, cl *kgo.Client, topic string, partitions int32) error {
	retention := strconv.FormatInt(time.Hour.Milliseconds(), 10)
	segment := strconv.FormatInt((10 * time.Minute).Milliseconds(), 10)
	adm := kadm.NewClient(cl)
	for {
		_, err := adm.CreateTopic(ctx, partitions, -1, map[string]*string{
			"retention.ms": &retention,
			"segment.ms":   &segment,
		}, topic)
		if err == nil || errors.Is(err, kerr.TopicAlreadyExists) {
			return nil
		}
		slog.Warn("create topic", "topic", topic, "err", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
