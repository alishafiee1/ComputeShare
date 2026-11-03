// Package quota counts space NIC rx+tx per local day (Asia/Tehran) (node 005 R3).
package quota

import (
	"time"

	"github.com/alishafiee1/computeshare-client/internal/node/config"
)

const File = "quota.json"

var Zone, _ = time.LoadLocation("Asia/Tehran")

type Counter struct {
	Day     string `json:"day"`
	Used    int64  `json:"used"`
	LastRaw int64  `json:"last_raw"`
	Reached bool   `json:"reached"`
}

func Load() Counter {
	var c Counter
	config.ReadJSON(File, &c)
	return c
}

func (c Counter) Save() error { return config.WriteJSON(File, c) }

func Today(now time.Time) string {
	if Zone == nil {
		return now.Format("2006-01-02")
	}
	return now.In(Zone).Format("2006-01-02")
}

// Update adds the delta of the cumulative raw counter (rx+tx); handles day rollover and counter resets.
// limit <= 0 means unlimited. Returns whether the quota is reached now.
func (c *Counter) Update(now time.Time, raw, limit int64) bool {
	d := Today(now)
	if c.Day != d {
		c.Day, c.Used, c.Reached = d, 0, false
	}
	if raw >= c.LastRaw && c.LastRaw > 0 {
		c.Used += raw - c.LastRaw
	}
	c.LastRaw = raw
	c.Reached = limit > 0 && c.Used >= limit
	return c.Reached
}
