package bench

import (
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	r, err := Parse("Welcome!\n{\"version\":\"1\",\"cpus\":4,\"cpu_single\":120.5,\"cpu_all\":470,\"ram_gbs\":9.1,\"disk_write_mbs\":800,\"disk_read_mbs\":1200,\"disk_direct\":true,\"gpu\":\"none\",\"seconds\":14.2}\n")
	if err != nil || r.CPUs != 4 || r.CPUAll != 470 || r.GPU != "none" {
		t.Fatal(r, err)
	}
	if _, err := Parse("bash: cs-bench: not found"); err == nil {
		t.Fatal("garbage parsed")
	}
	if _, err := Parse(`{"version":"1"}`); err == nil {
		t.Fatal("incomplete result accepted")
	}
}

// R6: one run per node per 10 minutes from one machine.
func TestGap(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	if CheckGap(nil, now) != nil || CheckGap(&Result{At: now.Add(-11 * time.Minute)}, now) != nil {
		t.Fatal("old run blocked")
	}
	err := CheckGap(&Result{At: now.Add(-3*time.Minute - 10*time.Second)}, now)
	if err == nil || err.Error() != "last test was 3 min ago; try again in 7 min" {
		t.Fatal(err)
	}
}

func TestStoreAndFormat(t *testing.T) {
	d := t.TempDir()
	now := time.Now()
	r := &Result{Node: "Box", At: now.Add(-5 * time.Minute), CPUs: 8, CPUSingle: 100, CPUAll: 700, RAMGBs: 12, DiskWriteMBs: 500, DiskReadMBs: 900, GPU: "none", Seconds: 14}
	if err := Save(d, r); err != nil {
		t.Fatal(err)
	}
	got := Last(d, "box")
	if got == nil || got.CPUAll != 700 {
		t.Fatal(got)
	}
	f := got.Format(now)
	for _, w := range []string{"5 min ago", "100.0 single / 700.0 all 8 threads", "12.0 GB/s", "500 MB/s write, 900 MB/s read (cached)", "gpu:       none"} {
		if !strings.Contains(f, w) {
			t.Fatalf("missing %q in\n%s", w, f)
		}
	}
}
