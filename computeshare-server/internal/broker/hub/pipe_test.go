package hub

import (
	"io"
	"net"
	"testing"
	"time"
)

// card 021 R3: a pipe with no traffic is closed with reason idle, and both sides see EOF.
func TestPipeWatchIdle(t *testing.T) {
	c1, c2 := net.Pipe() // c1: test side of the client
	n1, n2 := net.Pipe() // n1: test side of the node
	type res struct {
		up, down int64
		why      string
	}
	done := make(chan res, 1)
	go func() {
		u, d, w := PipeWatch(c2, n2, PipeOptions{Idle: 80 * time.Millisecond})
		done <- res{u, d, w}
	}()
	c1.Write([]byte("hi"))
	buf := make([]byte, 2)
	io.ReadFull(n1, buf)
	select {
	case r := <-done:
		if r.why != EndIdle || r.up != 2 {
			t.Fatalf("got %+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("idle session was not closed")
	}
	if _, err := c1.Read(make([]byte, 1)); err == nil {
		t.Fatal("client side still open")
	}
}

// card 021 R3: traffic keeps a session alive past the idle limit.
func TestPipeWatchActiveSurvives(t *testing.T) {
	c1, c2 := net.Pipe()
	n1, n2 := net.Pipe()
	done := make(chan string, 1)
	go func() { _, _, w := PipeWatch(c2, n2, PipeOptions{Idle: 150 * time.Millisecond}); done <- w }()
	go io.Copy(io.Discard, n1)
	for i := 0; i < 8; i++ {
		c1.Write([]byte("x"))
		time.Sleep(50 * time.Millisecond)
	}
	select {
	case w := <-done:
		t.Fatalf("closed while active: %s", w)
	default:
	}
	c1.Close()
	select {
	case w := <-done:
		if w != EndClosed {
			t.Fatalf("reason %q", w)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("not closed after the client closed")
	}
}

// card 021 R3: a peer that stops reading (blocked write) is closed as stalled.
func TestPipeWatchStalled(t *testing.T) {
	c1, c2 := net.Pipe() // nobody reads c1, so writes to the client block
	n1, n2 := net.Pipe()
	done := make(chan string, 1)
	go func() { _, _, w := PipeWatch(c2, n2, PipeOptions{Stall: 100 * time.Millisecond}); done <- w }()
	go n1.Write([]byte("data for a client that never reads"))
	select {
	case w := <-done:
		if w != EndStalled {
			t.Fatalf("reason %q", w)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stalled session was not closed")
	}
	_ = c1
}

func TestPipeWatchNoLimits(t *testing.T) {
	c1, c2 := net.Pipe()
	n1, n2 := net.Pipe()
	done := make(chan string, 1)
	go func() { _, _, w := PipeWatch(c2, n2, PipeOptions{}); done <- w }()
	go io.Copy(io.Discard, n1)
	c1.Write([]byte("x"))
	c1.Close()
	select {
	case w := <-done:
		if w != EndClosed {
			t.Fatalf("reason %q", w)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("did not end")
	}
}
