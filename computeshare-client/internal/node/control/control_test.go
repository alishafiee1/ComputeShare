package control

import (
	"testing"

	"github.com/alishafiee1/computeshare-client/internal/node/config"
)

type rec struct{ calls []string }

func (r *rec) SetCPU(i, v string) error       { r.calls = append(r.calls, "cpu="+v); return nil }
func (r *rec) SetMemory(i, v string) error    { r.calls = append(r.calls, "mem="+v); return nil }
func (r *rec) SetBandwidth(n, v string) error { r.calls = append(r.calls, "bw="+v); return nil }

func s(v string) *string { return &v }

func TestApplyLimits(t *testing.T) {
	t.Setenv("CSNODE_DIR", t.TempDir())
	c := &config.Config{Name: "myBrotherPc", Space: config.Space{Instance: "cs-myBrotherPc"}}
	r := &rec{}
	if err := ApplyLimits(c, Limits{CPU: s("4"), Memory: s("8GiB"), Bandwidth: s("20Mbit"), Quota: s("20GB/day")}, r, true); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 3 || c.Limits.Quota != "20GB/day" {
		t.Fatal(r.calls, c.Limits)
	}
	// "off" and empty mean unlimited
	if err := ApplyLimits(c, Limits{Bandwidth: s("off"), Quota: s("")}, r, true); err != nil || c.Limits.Bandwidth != "" || c.Limits.Quota != "" {
		t.Fatal(err, c.Limits)
	}
	got, _ := config.Load()
	if got.Limits.CPU != "4" || got.Limits.Memory != "8GiB" {
		t.Fatal("not saved", got.Limits)
	}
	for _, bad := range []Limits{{CPU: s("0")}, {CPU: s("x")}, {Memory: s("8 gigs")}, {Bandwidth: s("fast")}, {Quota: s("lots")}} {
		if err := ApplyLimits(c, bad, r, true); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
}

func TestShare(t *testing.T) {
	t.Setenv("CSNODE_DIR", t.TempDir())
	if !Sharing() {
		t.Fatal("default should share")
	}
	SetShare(false)
	if Sharing() {
		t.Fatal("still sharing")
	}
	SetShare(true)
	if !Sharing() {
		t.Fatal("not sharing")
	}
}
