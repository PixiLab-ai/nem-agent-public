package desksync

import (
	"context"
	"sync"
	"time"
)

// pacer keeps the folder's API calls under a rate: every key of the account shares one bucket on
// the server (300, then 5 a second), the app's own window included, so the sync takes far less
// and the human's clicks never wait behind it.
type pacer struct {
	mu     sync.Mutex
	rate   float64 // calls per second
	burst  float64
	tokens float64
	last   time.Time
}

func newPacer(rate, burst float64) *pacer {
	return &pacer{rate: rate, burst: burst, tokens: burst, last: time.Now()}
}

// wait takes one call's token, waiting for it if needed.
func (p *pacer) wait(ctx context.Context) error {
	for {
		p.mu.Lock()
		now := time.Now()
		p.tokens += now.Sub(p.last).Seconds() * p.rate
		if p.tokens > p.burst {
			p.tokens = p.burst
		}
		p.last = now
		if p.tokens >= 1 {
			p.tokens--
			p.mu.Unlock()
			return nil
		}
		need := time.Duration((1 - p.tokens) / p.rate * float64(time.Second))
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(need):
		}
	}
}

// hold makes the next calls wait (the server said 429: retry after d).
func (p *pacer) hold(d time.Duration) {
	p.mu.Lock()
	p.tokens = -d.Seconds() * p.rate
	p.last = time.Now()
	p.mu.Unlock()
}
