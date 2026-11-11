package tunnel

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestBackoff(t *testing.T) {
	for n := 0; n < 12; n++ {
		d := Backoff(n, time.Second, 120*time.Second)
		if d < 750*time.Millisecond || d > 150*time.Second {
			t.Fatal(n, d)
		}
	}
	if Backoff(20, time.Second, 120*time.Second) < 90*time.Second {
		t.Fatal("cap")
	}
}

func TestPendingBackoffCapped(t *testing.T) {
	pend := fmt.Errorf("%w: wait", ErrPending)
	for n := 0; n < 20; n++ {
		if d := retryDelay(pend, n, time.Second, 30*time.Second); d > PendingMaxBack*5/4 {
			t.Fatal(n, d)
		}
	}
	big := false
	for n := 0; n < 20; n++ {
		if retryDelay(errors.New("x"), 10, time.Second, 30*time.Second) > 15*time.Second {
			big = true
		}
	}
	if !big {
		t.Fatal("normal errors must keep the long backoff")
	}
}
