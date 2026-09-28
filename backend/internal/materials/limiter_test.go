package materials

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestParseLimiterAdmitsOneAtATime(t *testing.T) {
	limiter := NewParseLimiter(1, 20*time.Millisecond)

	if err := limiter.Acquire(context.Background()); err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	defer limiter.Release()

	if err := limiter.Acquire(context.Background()); !errors.Is(err, ErrParserBusy) {
		t.Errorf("second Acquire() error = %v, want ErrParserBusy", err)
	}
}

func TestParseLimiterFreesTheSlotOnRelease(t *testing.T) {
	limiter := NewParseLimiter(1, 50*time.Millisecond)

	if err := limiter.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	limiter.Release()

	if err := limiter.Acquire(context.Background()); err != nil {
		t.Errorf("Acquire() after Release() error = %v, want nil", err)
	}
	limiter.Release()
}

func TestParseLimiterHonoursCancellation(t *testing.T) {
	limiter := NewParseLimiter(1, time.Hour)

	if err := limiter.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	defer limiter.Release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := limiter.Acquire(ctx); !errors.Is(err, ErrParserBusy) {
		t.Errorf("Acquire() on a cancelled context error = %v, want ErrParserBusy", err)
	}
}

// A nil limiter is unlimited, so a Handlers zero value stays usable.
func TestNilParseLimiterIsUnlimited(t *testing.T) {
	var limiter *ParseLimiter

	for i := 0; i < 3; i++ {
		if err := limiter.Acquire(context.Background()); err != nil {
			t.Fatalf("Acquire() on a nil limiter error = %v", err)
		}
	}

	limiter.Release()
}
