package materials

import (
	"context"
	"fmt"
	"time"
)

// ParseLimiter bounds how many document parsers run at once.
//
// Parsing a PDF or DOCX is the expensive part of an upload and the API
// container is capped at one CPU, so only one runs at a time. A request that
// cannot get a slot within Wait is answered with 503 rather than queueing
// without bound.
type ParseLimiter struct {
	slots chan struct{}
	wait  time.Duration
}

// NewParseLimiter builds a limiter allowing concurrency simultaneous parses,
// each waiting at most wait for a slot before giving up.
func NewParseLimiter(concurrency int, wait time.Duration) *ParseLimiter {
	if concurrency < 1 {
		concurrency = 1
	}

	return &ParseLimiter{slots: make(chan struct{}, concurrency), wait: wait}
}

// Acquire takes a parse slot. It returns ErrParserBusy if the slot does not
// become available in time, or if the request is abandoned while waiting.
//
// A nil limiter is unlimited, which keeps the zero value of Handlers usable.
func (l *ParseLimiter) Acquire(ctx context.Context) error {
	if l == nil {
		return nil
	}

	timer := time.NewTimer(l.wait)
	defer timer.Stop()

	select {
	case l.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%w: the request was abandoned while waiting", ErrParserBusy)
	case <-timer.C:
		return ErrParserBusy
	}
}

// Release returns a slot to the pool. It must be called exactly once for each
// successful Acquire.
func (l *ParseLimiter) Release() {
	if l == nil {
		return
	}

	<-l.slots
}
