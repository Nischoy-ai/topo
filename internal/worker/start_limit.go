package worker

import (
	"context"
	"sync"
	"time"
)

// remoteStartLimiter spaces target attempts across all protocols in one worker.
// It has no burst credit; both queued and active waits are cancellable.
type remoteStartLimiter struct {
	once     sync.Once
	gate     chan struct{}
	next     time.Time
	interval time.Duration
}

func (l *remoteStartLimiter) wait(ctx context.Context) error {
	l.once.Do(func() { l.gate = make(chan struct{}, 1); l.gate <- struct{}{} })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-l.gate:
	}
	defer func() { l.gate <- struct{}{} }()
	timer := time.NewTimer(time.Until(l.next))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		if err := ctx.Err(); err != nil {
			return err
		}
		l.next = time.Now().Add(l.interval)
		return nil
	}
}
