package load

import (
	"math/rand/v2"
	"runtime"
	"sync"
	"time"

	"github.com/Aset-Mk/geomatch/internal/index"
	"github.com/Aset-Mk/geomatch/internal/sim"
)

// RunOrders calls idx.Nearest rate times per second for dur
func RunOrders(idx index.Index, b sim.Bounds, r *rand.Rand, rate int, dur time.Duration, record func(time.Duration)) {
	interval := time.Second / time.Duration(rate)
	n := int(dur / interval)
	start := time.Now()

	for i := range n {
		scheduled := start.Add(time.Duration(i) * interval)
		waitUntil(scheduled)

		idx.Nearest(b.RandomPoint(r), 5)
		record(time.Since(scheduled))
	}
}

// RunOrdersAsync is RunOrders that does not wait for answers, with inflight orders unanswered the schedule falls behind
func RunOrdersAsync(idx index.Index, b sim.Bounds, r *rand.Rand, rate int, dur time.Duration, inflight int, record func(time.Duration)) {
	interval := time.Second / time.Duration(rate)
	n := int(dur / interval)
	start := time.Now()

	var mu sync.Mutex
	sem := make(chan struct{}, inflight)
	for i := range n {
		scheduled := start.Add(time.Duration(i) * interval)
		waitUntil(scheduled)

		p := b.RandomPoint(r)
		sem <- struct{}{}
		go func() {
			idx.Nearest(p, 5)
			d := time.Since(scheduled)
			mu.Lock()
			record(d)
			mu.Unlock()
			<-sem
		}()
	}
	for range inflight {
		sem <- struct{}{}
	}
}

// time.Sleep can wake up 1ms late, so the last spinWindow before t is busy-waited
const spinWindow = 2 * time.Millisecond

// spinning without yielding would keep goroutines woken on this P from running for milliseconds
const yieldUntil = 5 * time.Microsecond

func waitUntil(t time.Time) {
	if d := time.Until(t) - spinWindow; d > 0 {
		time.Sleep(d)
	}
	for {
		d := time.Until(t)
		if d <= 0 {
			return
		}
		if d > yieldUntil {
			runtime.Gosched()
		}
	}
}
