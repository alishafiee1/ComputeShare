// Package trust stores owner-given fingerprints and TOFU device-key pins (node 002 R4, R5).
package trust

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type Store struct {
	Fingerprints map[string]string `json:"fingerprints"` // name(lower) -> SHA256:...
	DeviceKeys   map[string]string `json:"device_keys"`  // name(lower) -> ed25519:...
	path         string
}

func Load(dir string) *Store {
	s := &Store{Fingerprints: map[string]string{}, DeviceKeys: map[string]string{}, path: filepath.Join(dir, "trust.json")}
	if b, err := os.ReadFile(s.path); err == nil {
		json.Unmarshal(b, s)
		if s.Fingerprints == nil {
			s.Fingerprints = map[string]string{}
		}
		if s.DeviceKeys == nil {
			s.DeviceKeys = map[string]string{}
		}
	}
	return s
}

func (s *Store) Save() error {
	os.MkdirAll(filepath.Dir(s.path), 0o700)
	b, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(s.path, b, 0o600)
}

func key(n string) string { return strings.ToLower(n) }

func (s *Store) Trust(name, fp string)          { s.Fingerprints[key(name)] = fp }
func (s *Store) Fingerprint(name string) string { return s.Fingerprints[key(name)] }
func (s *Store) Pin(name, dev string)           { s.DeviceKeys[key(name)] = dev }
func (s *Store) Pinned(name string) string      { return s.DeviceKeys[key(name)] }
