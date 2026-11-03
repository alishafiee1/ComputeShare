package tunnel

import (
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
