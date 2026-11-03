package stream

import (
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alishafiee1/computeshare-client/proto"
)

func TestOnlyTarget(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	go func() { c, _ := l.Accept(); c.Write([]byte("SSH-2.0-space\r\n")); c.Close() }()
	dialed := ""
	h := &Handler{Target: func() (string, error) { dialed = l.Addr().String(); return dialed, nil }, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	a, b := net.Pipe()
	go h.Handle(b)
	a.Write([]byte(`{"stream_id":3,"kind":"ssh","via":"jump","src_ip":"1.2.3.4","started_at":1}` + "\n"))
	buf, _ := io.ReadAll(a)
	if !strings.HasPrefix(string(buf), "SSH-2.0-space") || dialed != l.Addr().String() {
		t.Fatal(string(buf))
	}
}

func TestGateRefuses(t *testing.T) {
	called := false
	h := &Handler{Target: func() (string, error) { called = true; return "", nil }, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Gates: []Gate{func(*proto.StreamOpen) (bool, string) { return false, "node paused" }}}
	a, b := net.Pipe()
	go h.Handle(b)
	a.Write([]byte(`{"stream_id":3,"kind":"ssh"}` + "\n"))
	buf, _ := io.ReadAll(a)
	if called || !strings.Contains(string(buf), "node paused") {
		t.Fatal(string(buf))
	}
}

func TestBadKind(t *testing.T) {
	if _, err := ReadOpen(strings.NewReader(`{"kind":"tcp"}` + "\n")); err == nil {
		t.Fatal("non-ssh kind accepted")
	}
}

func TestKick(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go io.Copy(c, c) // echo "space"; stays open until kicked
		}
	}()
	h := &Handler{Target: func() (string, error) { return l.Addr().String(), nil }, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	open := func(id int, ip string) net.Conn {
		a, b := net.Pipe()
		go h.Handle(b)
		a.Write([]byte(`{"stream_id":` + string(rune('0'+id)) + `,"kind":"ssh","via":"jump","src_ip":"` + ip + `"}` + "\n"))
		a.Write([]byte("x"))
		buf := make([]byte, 1)
		io.ReadFull(a, buf)
		return a
	}
	a1, a2, a3 := open(1, "1.1.1.1"), open(2, "2.2.2.2"), open(3, "2.2.2.2")
	if len(h.Sessions()) != 3 {
		t.Fatal(h.Sessions())
	}
	if !h.Kick(1) || h.Kick(9) {
		t.Fatal("kick by id")
	}
	if _, err := io.ReadAll(a1); err != nil {
		t.Fatal(err)
	}
	if n := h.KickIP("2.2.2.2"); n != 2 {
		t.Fatal("kick ip", n)
	}
	io.ReadAll(a2)
	io.ReadAll(a3)
	for i := 0; i < 50 && len(h.Sessions()) > 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if len(h.Sessions()) != 0 {
		t.Fatal("sessions left", h.Sessions())
	}
}

// logBuf collects log lines so tests can check the "session end" reason.
type logBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logBuf) Write(p []byte) (int, error) { l.mu.Lock(); defer l.mu.Unlock(); return l.b.Write(p) }
func (l *logBuf) String() string              { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func echoSpace(t *testing.T) net.Listener {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go io.Copy(c, c)
		}
	}()
	return l
}

func waitEmpty(t *testing.T, h *Handler) {
	t.Helper()
	for i := 0; i < 300 && len(h.Sessions()) > 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if len(h.Sessions()) != 0 {
		t.Fatal("sessions left", h.Sessions())
	}
}

// umbrella card 021 R3, R1: an idle session is closed with reason idle and leaves no entry behind.
func TestIdleSessionClosed(t *testing.T) {
	l := echoSpace(t)
	defer l.Close()
	lb := &logBuf{}
	h := &Handler{Target: func() (string, error) { return l.Addr().String(), nil }, Idle: 100 * time.Millisecond,
		Log: slog.New(slog.NewTextHandler(lb, nil))}
	a, b := net.Pipe()
	go h.Handle(b)
	a.Write([]byte(`{"stream_id":5,"kind":"ssh","via":"jump","src_ip":"9.9.9.9"}` + "\n"))
	a.Write([]byte("x"))
	io.ReadFull(a, make([]byte, 1))
	if _, err := io.ReadAll(a); err != nil {
		t.Fatal(err)
	}
	waitEmpty(t, h)
	if !strings.Contains(lb.String(), "reason=idle") {
		t.Fatal(lb.String())
	}
}

// umbrella card 021 R1: a kick is logged as reason kick and the entry is gone in well under 5 s.
func TestKickReasonAndCounters(t *testing.T) {
	l := echoSpace(t)
	defer l.Close()
	lb := &logBuf{}
	h := &Handler{Target: func() (string, error) { return l.Addr().String(), nil }, Log: slog.New(slog.NewTextHandler(lb, nil))}
	a, b := net.Pipe()
	go h.Handle(b)
	a.Write([]byte(`{"stream_id":6,"kind":"ssh","via":"jump","src_ip":"8.8.8.8"}` + "\n"))
	a.Write([]byte("hello"))
	io.ReadFull(a, make([]byte, 5))
	ss := h.Sessions()
	if len(ss) != 1 || ss[0].BytesUp != 5 || ss[0].BytesDown != 5 {
		t.Fatalf("counters: %+v", ss)
	}
	h.Kick(6)
	io.ReadAll(a)
	waitEmpty(t, h)
	if !strings.Contains(lb.String(), "reason=kick") || !strings.Contains(lb.String(), "bytes_up=5") {
		t.Fatal(lb.String())
	}
}

// umbrella card 021 R3: a client that stops reading is closed as stalled once a write blocks too long.
func TestStalledSessionClosed(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		c.Write([]byte(strings.Repeat("z", 1<<16))) // the "space" talks, the client never reads
		time.Sleep(5 * time.Second)
		c.Close()
	}()
	lb := &logBuf{}
	h := &Handler{Target: func() (string, error) { return l.Addr().String(), nil }, Stall: 100 * time.Millisecond, Idle: -1,
		Log: slog.New(slog.NewTextHandler(lb, nil))}
	a, b := net.Pipe()
	defer a.Close()
	go h.Handle(b)
	a.Write([]byte(`{"stream_id":7,"kind":"ssh","via":"jump","src_ip":"7.7.7.7"}` + "\n"))
	for i := 0; i < 300 && len(h.Sessions()) == 0; i++ {
		time.Sleep(5 * time.Millisecond) // wait until the session is tracked
	}
	if len(h.Sessions()) == 0 {
		t.Fatal("session never started")
	}
	waitEmpty(t, h)
	if !strings.Contains(lb.String(), "reason=stalled") {
		t.Fatal(lb.String())
	}
}

// client 016 R1: the node refuses streams over its own cap, whatever the broker allows.
func TestMaxStreams(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go io.Copy(c, c)
		}
	}()
	h := &Handler{Target: func() (string, error) { return l.Addr().String(), nil }, MaxStreams: 2,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	open := func(id string) net.Conn {
		a, b := net.Pipe()
		go h.Handle(b)
		a.Write([]byte(`{"stream_id":` + id + `,"kind":"ssh","src_ip":"1.1.1.1"}` + "\n"))
		return a
	}
	for _, id := range []string{"1", "2"} {
		a := open(id)
		a.Write([]byte("x"))
		buf := make([]byte, 1)
		io.ReadFull(a, buf)
	}
	buf, _ := io.ReadAll(open("3"))
	if !strings.Contains(string(buf), "node busy") {
		t.Fatalf("3rd stream: %q", buf)
	}
	h.Kick(1)
	time.Sleep(50 * time.Millisecond)
	a := open("4")
	a.Write([]byte("y"))
	b := make([]byte, 1)
	if _, err := io.ReadFull(a, b); err != nil || b[0] != 'y' {
		t.Fatal("slot not freed after a session ended", err)
	}
}
