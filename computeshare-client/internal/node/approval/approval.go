// Package approval implements owner approval (node 005 R4, R5). Data stays on the node (R8).
package approval

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/alishafiee1/computeshare-client/internal/node/config"
)

const File = "approvals.json"

type Pending struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"` // "key" | "session"
	Fingerprint string    `json:"fingerprint,omitempty"`
	SrcIP       string    `json:"src_ip,omitempty"`
	User        string    `json:"user,omitempty"` // set for gateway logins (client 015 R9)
	At          time.Time `json:"at"`
	Decision    string    `json:"decision,omitempty"` // once|ip|deny
}

type IPGrant struct {
	IP    string    `json:"ip"`
	Until time.Time `json:"until"`
}

type Data struct {
	Keys    []string  `json:"keys"` // approved key fingerprints
	IPs     []IPGrant `json:"ips"`
	Pending []Pending `json:"pending"`
	// Decided holds owner answers for gateway logins until the waiting login reads them.
	Decided map[string]string `json:"decided,omitempty"`
}

var mu sync.Mutex

func Load() Data {
	var d Data
	config.ReadJSON(File, &d)
	return d
}

func Save(d Data) error { return config.WriteJSON(File, d) }

func newID() string {
	b := make([]byte, 3)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (d *Data) IPGranted(ip string, now time.Time) bool {
	for _, g := range d.IPs {
		if g.IP == ip && now.Before(g.Until) {
			return true
		}
	}
	return false
}

func (d *Data) KeyApproved(fp string) bool {
	for _, k := range d.Keys {
		if k == fp {
			return true
		}
	}
	return false
}

// AddPendingKey records a key fingerprint once.
func AddPendingKey(fp string) {
	mu.Lock()
	defer mu.Unlock()
	d := Load()
	for _, p := range d.Pending {
		if p.Kind == "key" && p.Fingerprint == fp {
			return
		}
	}
	d.Pending = append(d.Pending, Pending{ID: newID(), Kind: "key", Fingerprint: fp, At: time.Now()})
	Save(d)
}

// AddPendingSession records a held password session (id "s<port>") reported by the space.
func AddPendingSession(id, ip string) {
	mu.Lock()
	defer mu.Unlock()
	d := Load()
	for _, p := range d.Pending {
		if p.ID == id {
			return
		}
	}
	d.Pending = append(d.Pending, Pending{ID: id, Kind: "session", SrcIP: ip, At: time.Now()})
	Save(d)
}

// AddGateway records a held gateway login (kind "key" with fingerprint, or "session" with IP)
// and returns its id (client 015 R9).
func AddGateway(kind, user, fp, ip string) string {
	mu.Lock()
	defer mu.Unlock()
	d := Load()
	for _, p := range d.Pending {
		if p.User == user && p.Kind == kind && p.Fingerprint == fp && p.SrcIP == ip {
			return p.ID
		}
	}
	id := "g" + newID()
	d.Pending = append(d.Pending, Pending{ID: id, Kind: kind, User: user, Fingerprint: fp, SrcIP: ip, At: time.Now()})
	Save(d)
	return id
}

// TakeDecision returns and clears the owner's answer for a gateway login ("" = none yet).
func TakeDecision(id string) string {
	mu.Lock()
	defer mu.Unlock()
	d := Load()
	v := d.Decided[id]
	if v != "" {
		delete(d.Decided, id)
		Save(d)
	}
	return v
}

// Withdraw drops a pending gateway login that timed out.
func Withdraw(id string) {
	mu.Lock()
	defer mu.Unlock()
	d := Load()
	remove(&d, id)
	delete(d.Decided, id)
	Save(d)
}

func (d *Data) decide(p Pending, v string) {
	if p.User == "" {
		return
	}
	if d.Decided == nil {
		d.Decided = map[string]string{}
	}
	d.Decided[p.ID] = v
}

// DropStaleSessions removes session entries the space no longer holds (timeout / decided).
func DropStaleSessions(live map[string]bool) {
	mu.Lock()
	defer mu.Unlock()
	d := Load()
	out := d.Pending[:0]
	for _, p := range d.Pending {
		if p.Kind != "session" || p.User != "" || live[p.ID] {
			out = append(out, p)
		}
	}
	d.Pending = out
	Save(d)
}

func remove(d *Data, id string) {
	out := d.Pending[:0]
	for _, p := range d.Pending {
		if p.ID != id {
			out = append(out, p)
		}
	}
	d.Pending = out
}

// Approve applies an owner decision. once=true lets one session through (session kind only).
// For a key it adds the fingerprint; for a session without --once it grants the IP for 24 h.
func Approve(id string, once bool) (*Pending, error) {
	mu.Lock()
	defer mu.Unlock()
	d := Load()
	for i, p := range d.Pending {
		if p.ID != id {
			continue
		}
		switch p.Kind {
		case "key":
			if p.User != "" {
				d.decide(p, "key") // the caller stores the fingerprint for that user in users.json
			} else if !d.KeyApproved(p.Fingerprint) {
				d.Keys = append(d.Keys, p.Fingerprint)
			}
			remove(&d, id)
		case "session":
			if once {
				p.Decision = "once"
				d.decide(p, "once")
			} else {
				d.decide(p, "ip")
				p.Decision = "ip"
				d.IPs = append(d.IPs, IPGrant{IP: p.SrcIP, Until: time.Now().Add(24 * time.Hour)})
			}
			remove(&d, id)
		}
		_ = i
		return &p, Save(d)
	}
	return nil, errNotFound
}

func Deny(id string) error {
	mu.Lock()
	defer mu.Unlock()
	d := Load()
	for _, p := range d.Pending {
		if p.ID == id {
			d.decide(p, "deny")
			remove(&d, id)
			return Save(d)
		}
	}
	return errNotFound
}

type nf struct{}

func (nf) Error() string { return "no such pending request" }

var errNotFound = nf{}
