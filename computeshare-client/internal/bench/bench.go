// Package bench holds the cs bench result format, local storage and the run limit (client 018).
package bench

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Result is the one JSON line the helper prints (R3); cs adds Node and At when storing (R5).
type Result struct {
	Node         string    `json:"node,omitempty"`
	At           time.Time `json:"at,omitempty"`
	Version      string    `json:"version"`
	CPUs         int       `json:"cpus"`
	CPUSingle    float64   `json:"cpu_single"` // million loop rounds per second, one thread
	CPUAll       float64   `json:"cpu_all"`    // same, all threads
	RAMGBs       float64   `json:"ram_gbs"`
	DiskWriteMBs float64   `json:"disk_write_mbs"`
	DiskReadMBs  float64   `json:"disk_read_mbs"`
	DiskDirect   bool      `json:"disk_direct"` // false = page cache could not be bypassed
	GPU          string    `json:"gpu"`
	Seconds      float64   `json:"seconds"`
	Error        string    `json:"error,omitempty"`
}

// Parse takes the helper's output and returns the last JSON result line.
func Parse(out string) (*Result, error) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(l, "{") {
			continue
		}
		var r Result
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			return nil, err
		}
		if r.CPUs == 0 {
			return nil, errors.New("benchmark result is missing fields")
		}
		return &r, nil
	}
	return nil, errors.New("no benchmark result in the output")
}

// MinGap is the time between two runs against one node from one machine (R6).
const MinGap = 10 * time.Minute

func path(dir, node string) string { return filepath.Join(dir, "bench", strings.ToLower(node)+".json") }

// Last returns the stored result for node, or nil.
func Last(dir, node string) *Result {
	b, err := os.ReadFile(path(dir, node))
	if err != nil {
		return nil
	}
	var r Result
	if json.Unmarshal(b, &r) != nil {
		return nil
	}
	return &r
}

// Save stores r as the last result for its node.
func Save(dir string, r *Result) error {
	p := path(dir, r.Node)
	os.MkdirAll(filepath.Dir(p), 0o700)
	b, _ := json.MarshalIndent(r, "", "  ")
	return os.WriteFile(p, b, 0o600)
}

// CheckGap returns an error with the R6 message when the last run is too recent.
func CheckGap(last *Result, now time.Time) error {
	if last == nil || last.At.IsZero() {
		return nil
	}
	ago := now.Sub(last.At)
	if ago >= MinGap {
		return nil
	}
	wait := int(math.Ceil((MinGap - ago).Minutes()))
	return fmt.Errorf("last test was %d min ago; try again in %d min", int(ago.Minutes()), wait)
}

// Age is a short "how long ago" for display.
func Age(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	}
	return fmt.Sprintf("%d days ago", int(d.Hours()/24))
}

// Format renders a result for the terminal.
func (r *Result) Format(now time.Time) string {
	direct := ""
	if !r.DiskDirect {
		direct = " (cached)"
	}
	s := fmt.Sprintf("test:      %s (%s, %.1f s)\ncpu:       %.1f single / %.1f all %d threads (M rounds/s)\nram:       %.1f GB/s copy\ndisk:      %.0f MB/s write, %.0f MB/s read%s\ngpu:       %s\n",
		r.At.Local().Format("2006-01-02 15:04"), Age(r.At, now), r.Seconds, r.CPUSingle, r.CPUAll, r.CPUs, r.RAMGBs, r.DiskWriteMBs, r.DiskReadMBs, direct, r.GPU)
	if r.Error != "" {
		s += "note:      " + r.Error + "\n"
	}
	return s
}
