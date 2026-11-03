package hub

import (
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// Why a piped session ended (umbrella card 021 R1).
const (
	EndClosed  = "closed"  // one side closed normally
	EndIdle    = "idle"    // no bytes in either direction for PipeOptions.Idle
	EndStalled = "stalled" // a write was blocked for longer than PipeOptions.Stall
)

// PipeOptions limits a piped session. A zero field turns that limit off.
type PipeOptions struct {
	Idle  time.Duration // close after this long without a byte in either direction
	Stall time.Duration // close when one write has been blocked this long (peer stopped reading)
}

type activity struct {
	last       atomic.Int64 // unix nano of the last byte moved
	writeStart atomic.Int64 // unix nano when the write in flight began, 0 if none
}

// watched wraps one side to record activity; CloseWrite stays available for half-close.
type watched struct {
	rw io.ReadWriteCloser
	a  *activity
}

func (w *watched) Read(p []byte) (int, error) {
	n, err := w.rw.Read(p)
	if n > 0 {
		w.a.last.Store(time.Now().UnixNano())
	}
	return n, err
}

func (w *watched) Write(p []byte) (int, error) {
	now := time.Now().UnixNano()
	w.a.writeStart.Store(now)
	n, err := w.rw.Write(p)
	w.a.writeStart.Store(0)
	if n > 0 {
		w.a.last.Store(time.Now().UnixNano())
	}
	return n, err
}

func (w *watched) Close() error      { return w.rw.Close() }
func (w *watched) CloseWrite() error { closeWrite(w.rw); return nil }

// PipeWatch is Pipe with an idle and a stall watchdog. Both sides are closed as soon as one
// limit is hit, so a half-open or stuck peer never leaves a session behind. It returns bytes
// up (client->node), bytes down, and why it ended.
func PipeWatch(client, node io.ReadWriteCloser, o PipeOptions) (up, down int64, reason string) {
	a := &activity{}
	a.last.Store(time.Now().UnixNano())
	c, n := &watched{client, a}, &watched{node, a}
	var why atomic.Value
	why.Store(EndClosed)
	stop := make(chan struct{})
	var once sync.Once
	closeBoth := func(r string) {
		once.Do(func() {
			why.Store(r)
			client.Close()
			node.Close()
		})
	}
	if o.Idle > 0 || o.Stall > 0 {
		tick := time.Second
		for _, d := range []time.Duration{o.Idle, o.Stall} {
			if d > 0 && d/4 < tick {
				tick = d / 4
			}
		}
		if tick < time.Millisecond {
			tick = time.Millisecond
		}
		go func() {
			t := time.NewTicker(tick)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
				}
				now := time.Now().UnixNano()
				if ws := a.writeStart.Load(); o.Stall > 0 && ws != 0 && time.Duration(now-ws) > o.Stall {
					closeBoth(EndStalled)
					return
				}
				if o.Idle > 0 && time.Duration(now-a.last.Load()) > o.Idle {
					closeBoth(EndIdle)
					return
				}
			}
		}()
	}
	up, down = Pipe(c, n)
	close(stop)
	return up, down, why.Load().(string)
}
