// Package config holds csnode's config.yaml, device key and runtime state files (data-model.md, Node).
package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type Space struct {
	Instance     string `yaml:"instance"`
	User         string `yaml:"user"`
	Image        string `yaml:"image"`
	Mode         string `yaml:"mode"`
	PasswordAuth bool   `yaml:"password_auth"`
	// ImageFingerprint records an owner-chosen image outside images.lock (client 014 R5).
	ImageFingerprint string `yaml:"image_fingerprint,omitempty"`
	// Gateway is csbr0's address, written by setup (client 016 R3); empty on older nodes.
	Gateway string `yaml:"gateway,omitempty"`
	// Target overrides the stream destination. Lab/testing only; production
	// derives <space-ip>:22 from Incus and this must stay empty.
	Target string `yaml:"target,omitempty"`
}

type Limits struct {
	CPU       string `yaml:"cpu"`
	Memory    string `yaml:"memory"`
	Disk      string `yaml:"disk"`
	GPU       bool   `yaml:"gpu"`
	Bandwidth string `yaml:"bandwidth"`
	Quota     string `yaml:"quota"`
}

type Config struct {
	Server   string `yaml:"server"`
	CAFile   string `yaml:"ca_file,omitempty"`
	Name     string `yaml:"name"`
	NodeID   string `yaml:"node_id,omitempty"`
	Space    Space  `yaml:"space"`
	Limits   Limits `yaml:"limits"`
	Approval bool   `yaml:"approval"`
	Mirror   string `yaml:"mirror,omitempty"`
	// Panel is the local web panel address (loopback only); "off" disables it. Default 127.0.0.1:7777.
	Panel        string `yaml:"panel,omitempty"`
	RegisteredAt int64  `yaml:"registered_at,omitempty"`
	// MaxStreams caps concurrent sessions on this node (client 016 R1); 0 = 32.
	MaxStreams int `yaml:"max_streams,omitempty"`
	// SessionIdle closes a session with no traffic for this long ("15m" default, "off"); SessionStall
	// closes one whose write is blocked this long ("90s" default, "off"). Umbrella card 021 R3.
	SessionIdle  string `yaml:"session_idle,omitempty"`
	SessionStall string `yaml:"session_stall,omitempty"`
	// Gateway: per-user spaces through csnode's own SSH server (client 015).
	Gateway bool `yaml:"per_user_spaces,omitempty"`
	// IdleStop stops a user space with no session for this long ("30m" default, "off").
	IdleStop string `yaml:"idle_stop,omitempty"`
	// MaxRunningSpaces caps running user spaces (default 3).
	MaxRunningSpaces int `yaml:"max_running_spaces,omitempty"`
	// DefaultDisk is the root disk size for users without their own quota ("" = pool limit).
	DefaultDisk string `yaml:"default_disk,omitempty"`
}

// DefaultServer is the production registration/tunnel URL (nginx on 443 in front of csbroker).
const DefaultServer = "https://node.n3xus.ir"

// JumpHost returns the SSH jump host for a server URL: the URL's host name
// (all SSH goes through port 2222 on the same host).
func JumpHost(server string) string {
	h := server
	if i := strings.Index(h, "://"); i >= 0 {
		h = h[i+3:]
	}
	if i := strings.IndexAny(h, "/?#"); i >= 0 {
		h = h[:i]
	}
	if hh, _, err := net.SplitHostPort(h); err == nil {
		h = hh
	}
	return strings.Trim(h, "[]")
}

// Dir is the config directory (CSNODE_DIR overrides; default /etc/csnode or %ProgramData%\csnode).
func Dir() string {
	if d := os.Getenv("CSNODE_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("ProgramData"), "csnode")
	}
	return "/etc/csnode"
}

func path(n string) string { return filepath.Join(Dir(), n) }

func Load() (*Config, error) {
	b, err := readCached(path("config.yaml"))
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Save writes config.yaml with mode 0600 (R6). It never contains the space password.
func (c *Config) Save() error {
	os.MkdirAll(Dir(), 0o700)
	b, _ := yaml.Marshal(c)
	return writeFile0600(path("config.yaml"), b)
}

func writeFile0600(p string, b []byte) error {
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	os.Chmod(tmp, 0o600)
	cache.Lock()
	delete(cache.m, p)
	cache.Unlock()
	return os.Rename(tmp, p)
}

// DeviceKey loads or creates device.key (Ed25519 seed, mode 0600).
func DeviceKey() (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(path("device.key"))
	if err == nil {
		if len(b) != ed25519.SeedSize {
			return nil, errors.New("device.key corrupt")
		}
		return ed25519.NewKeyFromSeed(b), nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	os.MkdirAll(Dir(), 0o700)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	return priv, writeFile0600(path("device.key"), priv.Seed())
}

// State is runtime state shared between CLI invocations and the running agent.
type State struct {
	Paused  bool `json:"paused"`
	Stopped bool `json:"stopped"`
}

func LoadState() State {
	var s State
	b, _ := readCached(path("state.json"))
	json.Unmarshal(b, &s)
	return s
}

// readCached returns a file's bytes, re-reading only when its mtime or size changed, so the
// agent's per-second and per-stream checks cost a stat, not a read (client 016 R6).
// Writers replace files by rename, so a change always shows up as a new mtime.
var cache = struct {
	sync.Mutex
	m map[string]cached
}{m: map[string]cached{}}

type cached struct {
	mod  time.Time
	size int64
	b    []byte
}

func readCached(p string) ([]byte, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	cache.Lock()
	e, ok := cache.m[p]
	cache.Unlock()
	if ok && e.mod.Equal(fi.ModTime()) && e.size == fi.Size() {
		return e.b, nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	cache.Lock()
	cache.m[p] = cached{fi.ModTime(), fi.Size(), b}
	cache.Unlock()
	return b, nil
}

func (s State) Save() error {
	b, _ := json.Marshal(s)
	return writeFile0600(path("state.json"), b)
}

// JSON helpers for other node files (approvals.json, sessions.json, quota.json).
func ReadJSON(name string, v any) error {
	b, err := os.ReadFile(path(name))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func WriteJSON(name string, v any) error {
	os.MkdirAll(Dir(), 0o700)
	b, _ := json.MarshalIndent(v, "", "  ")
	return writeFile0600(path(name), b)
}

func Path(name string) string { return path(name) }
