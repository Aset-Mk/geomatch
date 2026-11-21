package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/Aset-Mk/geomatch/internal/feed"
	"github.com/Aset-Mk/geomatch/internal/index"
)

// Follow reads driver positions from topic into the index until ctx is done
// It starts from what was written ttl ago, older positions would be dropped as silent anyway
// ready is called once everything written before the start is in the index
func (s *Server) Follow(ctx context.Context, topic string, ttl time.Duration, ready func(), opts ...kgo.Opt) error {
	admin, err := kgo.NewClient(opts...)
	if err != nil {
		return err
	}
	defer admin.Close()
	adm := kadm.NewClient(admin)

	from := time.Now().Add(-ttl).UnixMilli()
	var starts, ends kadm.ListedOffsets
	// a topic created a moment ago may have no partition leaders yet
	for {
		starts, ends, err = bounds(ctx, adm, topic, from)
		if err == nil {
			break
		}
		slog.Warn("kafka offsets", "err", err)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Second):
		}
	}

	offsets := make(map[int32]kgo.Offset)
	behind := make(map[int32]int64) // end offset each partition has yet to reach
	for p, o := range starts[topic] {
		offsets[p] = kgo.NewOffset().At(o.Offset)
		if end := ends[topic][p].Offset; end > o.Offset {
			behind[p] = end
		}
	}

	cl, err := kgo.NewClient(append(opts, kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{topic: offsets}))...)
	if err != nil {
		return err
	}
	defer cl.Close()

	caughtUp := false
	lag := make(map[int32]int64)
	var batch []stamped
	for {
		if !caughtUp && len(behind) == 0 {
			caughtUp = true
			ready()
		}

		fs := cl.PollFetches(ctx)
		if ctx.Err() != nil {
			return nil
		}
		// the client retries what can be retried, the rest is lost data or a broken broker and is only logged
		fs.EachError(func(_ string, p int32, err error) {
			slog.Warn("kafka fetch", "partition", p, "err", err)
		})

		batch = batch[:0]
		fs.EachPartition(func(p kgo.FetchTopicPartition) {
			for _, r := range p.Records {
				id, pos, removed, err := feed.Parse(r)
				switch {
				case err != nil:
					badRecords.Inc()
				case removed:
					// positions read before the removal go in first
					s.applyRead(batch)
					batch = batch[:0]
					s.remove(id)
				default:
					batch = append(batch, stamped{index.Update{ID: id, Pos: pos}, r.Timestamp})
				}
			}
			if n := len(p.Records); n > 0 {
				next := p.Records[n-1].Offset + 1
				lag[p.Partition] = p.HighWatermark - next
				if end, ok := behind[p.Partition]; ok && next >= end {
					delete(behind, p.Partition)
				}
			}
		})
		s.applyRead(batch)

		total := int64(0)
		for _, n := range lag {
			total += n
		}
		kafkaLag.Set(float64(total))
	}
}

func (s *Server) applyRead(batch []stamped) {
	s.apply(batch)
	now := time.Now()
	for _, u := range batch {
		updateDelay.Observe(now.Sub(u.at).Seconds())
	}
}

// bounds lists where each partition of topic starts after from and where it ends now
func bounds(ctx context.Context, adm *kadm.Client, topic string, from int64) (starts, ends kadm.ListedOffsets, err error) {
	starts, err = adm.ListOffsetsAfterMilli(ctx, from, topic)
	if err == nil {
		err = starts.Error()
	}
	if err != nil {
		return nil, nil, err
	}
	ends, err = adm.ListEndOffsets(ctx, topic)
	if err == nil {
		err = ends.Error()
	}
	if err != nil {
		return nil, nil, err
	}
	if len(starts[topic]) == 0 {
		return nil, nil, fmt.Errorf("topic %s has no partitions", topic)
	}
	return starts, ends, nil
}
