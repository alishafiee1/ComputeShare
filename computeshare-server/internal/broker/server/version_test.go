package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alishafiee1/computeshare-client/proto"
)

// server 022 R1: GET /v1/version is public, JSON, not cacheable and shows only version and proto.
func TestVersionEndpoint(t *testing.T) {
	s := &Server{cfg: Config{Version: "0.2.0"}}
	rec := httptest.NewRecorder()
	s.handleVersion(rec, httptest.NewRequest(http.MethodGet, "/v1/version", nil))
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("code=%d cache=%q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["version"] != "0.2.0" || int(got["proto"].(float64)) != proto.Version {
		t.Fatalf("unexpected body: %v", got)
	}
}

func TestVersionUnknown(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleVersion(rec, httptest.NewRequest(http.MethodGet, "/v1/version", nil))
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["version"] != "unknown" {
		t.Fatalf("got %v", got)
	}
}
