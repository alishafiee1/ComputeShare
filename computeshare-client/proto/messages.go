// Package proto is the shared ComputeShare wire contract (protocol.md §1–§5).
package proto

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Version is the wire protocol version carried in every top-level message.
const Version = 2

// Error / result codes (R4).
const (
	CodeRegistered   = "REGISTERED"
	CodeNameTaken    = "NAME_TAKEN"
	CodeTokenInvalid = "TOKEN_INVALID"
	CodeNameInvalid  = "NAME_INVALID"
	CodeNodeBanned   = "NODE_BANNED"
	CodeNodePending  = "NODE_PENDING"
	CodeBadRequest   = "BAD_REQUEST"
	CodeClockSkew    = "CLOCK_SKEW"
	CodeRateLimited  = "RATE_LIMITED"
)

// MaxClockSkew is the max |now-ts| accepted for signed Hello/Leave (seconds).
const MaxClockSkew = 120

// Control message types (stream 0).
const (
	TypePing          = "Ping"
	TypePong          = "Pong"
	TypeStatus        = "Status"
	TypeLeave         = "Leave"
	TypeUpdateHostKey = "UpdateHostKey"
	TypeBan           = "Ban"
	TypeHello         = "Hello"
	TypeWelcome       = "Welcome"
	TypeError         = "Error"
)

type Specs struct {
	CPU    string  `json:"cpu,omitempty"`
	RAMGB  float64 `json:"ram_gb,omitempty"`
	GPU    string  `json:"gpu,omitempty"`
	DiskGB float64 `json:"disk_gb,omitempty"`
}

type Limits struct {
	Bandwidth string `json:"bandwidth"`
	Quota     string `json:"quota"`
}

type RegisterRequest struct {
	Proto        int    `json:"proto"`
	Name         string `json:"name"`
	Token        string `json:"token"`
	DevicePubkey string `json:"device_pubkey"`
	HostKey      string `json:"host_key"`
	HostKeySig   string `json:"host_key_sig"`
	RegisteredAt int64  `json:"registered_at"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	Specs        Specs  `json:"specs"`
	Limits       Limits `json:"limits"`
	Version      string `json:"version"`
}

// Validate checks required fields and formats (not the token or signature).
func (r *RegisterRequest) Validate() error {
	if r.Proto != Version {
		return fmt.Errorf("unsupported protocol %d (this build speaks %d): upgrade csnode/csbroker", r.Proto, Version)
	}
	var missing []string
	for k, v := range map[string]string{"name": r.Name, "token": r.Token, "device_pubkey": r.DevicePubkey, "host_key": r.HostKey, "host_key_sig": r.HostKeySig} {
		if v == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 || r.RegisteredAt == 0 {
		return fmt.Errorf("missing required fields: %v", missing)
	}
	return nil
}

type RegisterResponse struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
	NodeID  string `json:"node_id,omitempty"`
	// Status is "active" or "pending" (server-side admin approval enabled).
	Status string `json:"status,omitempty"`
}

// Hello is the first frame on /v1/tunnel; Sig = device signature over HelloBytes(node_id, ts).
type Hello struct {
	Proto  int    `json:"proto"`
	Type   string `json:"type"`
	NodeID string `json:"node_id"`
	TS     int64  `json:"ts"`
	Sig    string `json:"sig"`
}

type Welcome struct {
	Proto   int    `json:"proto"`
	Type    string `json:"type"`
	Name    string `json:"name,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type Stats struct {
	CPUPct  float64  `json:"cpu_pct"`
	RAMPct  float64  `json:"ram_pct"`
	GPUPct  *float64 `json:"gpu_pct,omitempty"`
	VRAMPct *float64 `json:"vram_pct,omitempty"`
	RxBps   float64  `json:"rx_bps"`
	TxBps   float64  `json:"tx_bps"`
	Load1   float64  `json:"load1"`
	Load5   float64  `json:"load5"`
	Load15  float64  `json:"load15"`
	// Shared is what the owner shares (card 016 R3): only the limited fields are set; an empty
	// field means "not limited", so the host value applies. Old brokers ignore it.
	Shared *Specs `json:"shared,omitempty"`
}

type Ping struct {
	Type  string `json:"type"`
	Stats Stats  `json:"stats"`
}
type Pong struct {
	Type string `json:"type"`
	TS   int64  `json:"ts"`
}
type Status struct {
	Type   string `json:"type"`
	Status string `json:"status"` // online|paused
}
type Leave struct {
	Type string `json:"type"`
	TS   int64  `json:"ts"`
	Sig  string `json:"sig"`
}
type UpdateHostKey struct {
	Type       string `json:"type"`
	HostKey    string `json:"host_key"`
	HostKeySig string `json:"host_key_sig"`
}
type Ban struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

// StreamOpen is the single JSON line the broker writes on every data stream before raw bytes.
type StreamOpen struct {
	StreamID  uint32 `json:"stream_id"`
	Kind      string `json:"kind"` // "ssh"
	Via       string `json:"via"`  // "jump" | "port"
	SrcIP     string `json:"src_ip"`
	StartedAt int64  `json:"started_at"`
}

type Load struct {
	Now    *float64 `json:"now"`
	Avg24h *float64 `json:"avg24h"`
}

// NodeRow is one row of `list json` and GET /v1/nodes (§4.3). Never contains credentials.
type NodeRow struct {
	Name         string   `json:"name"`
	Status       string   `json:"status"`
	OS           string   `json:"os"`
	Specs        Specs    `json:"specs"`
	Load         Load     `json:"load"`
	Limits       Limits   `json:"limits"`
	UptimeS      int64    `json:"uptime_s"`
	LastSeen     int64    `json:"last_seen"`
	HostKey      string   `json:"host_key"`
	HostKeySig   string   `json:"host_key_sig"`
	DevicePubkey string   `json:"device_pubkey"`
	RegisteredAt int64    `json:"registered_at"`
	GPUPct       *float64 `json:"gpu_pct,omitempty"`
	RAMPct       *float64 `json:"ram_pct,omitempty"`
	// Host holds the computer's own totals when Specs shows the shared amounts (card 016 R3);
	// Unlimited names the Specs fields ("cpu", "ram", "disk", "gpu") that show a host value because
	// the owner set no limit (R4). Both are absent for nodes that report no shared amounts.
	Host      *Specs   `json:"host,omitempty"`
	Unlimited []string `json:"unlimited,omitempty"`
}

// SharedView merges what the owner shares over the host totals the node registered with. It returns
// the specs to show, the host totals, and which fields are not limited. A nil shared returns host unchanged.
func SharedView(host Specs, shared *Specs) (show Specs, hostOut *Specs, unlimited []string) {
	if shared == nil {
		return host, nil, nil
	}
	show = host
	h := host
	if shared.CPU != "" {
		show.CPU = shared.CPU
	} else {
		unlimited = append(unlimited, "cpu")
	}
	if shared.RAMGB > 0 {
		show.RAMGB = shared.RAMGB
	} else {
		unlimited = append(unlimited, "ram")
	}
	if shared.DiskGB > 0 {
		show.DiskGB = shared.DiskGB
	} else {
		unlimited = append(unlimited, "disk")
	}
	show.GPU = shared.GPU // empty = the GPU is not shared
	return show, &h, unlimited
}

// Envelope is used to peek at the type of a control message.
type Envelope struct {
	Type string `json:"type"`
}

// PeekType returns the "type" of a JSON control line.
func PeekType(b []byte) (string, error) {
	var e Envelope
	if err := json.Unmarshal(b, &e); err != nil {
		return "", err
	}
	if e.Type == "" {
		return "", errors.New("missing type")
	}
	return e.Type, nil
}
