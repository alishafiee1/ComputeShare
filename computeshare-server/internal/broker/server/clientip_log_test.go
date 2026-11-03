package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alishafiee1/computeshare-server/internal/broker/netutil"
)

func TestLogAPIShowsClientIP(t *testing.T) {
	var buf bytes.Buffer
	tr, _ := netutil.ParseTrusted("172.18.0.0/16")
	s := &Server{Log: slog.New(slog.NewTextHandler(&buf, nil)), cfg: Config{TrustedProxies: tr}}
	h := s.logAPI(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := httptest.NewRequest("GET", "/v1/nodes", nil)
	r.RemoteAddr = "172.18.0.5:5555"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if !strings.Contains(buf.String(), "client_ip=203.0.113.9") || !strings.Contains(buf.String(), "remote_addr=172.18.0.5:5555") {
		t.Fatal(buf.String())
	}
}
