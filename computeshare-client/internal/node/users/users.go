// Package users is the node-local account store for per-user spaces (client 015 R3).
// Accounts are created by the node owner; nothing here ever leaves the node.
package users

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/ssh"

	"github.com/alishafiee1/computeshare-client/internal/node/config"
)

const File = "users.json"

// MinPassword matches 001 R4.
const MinPassword = 12

var nameRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// Reserved names are refused (R2: root and system users never log in).
var reserved = map[string]bool{"root": true, "daemon": true, "bin": true, "sys": true, "nobody": true, "join": true}

var (
	ErrName     = errors.New("invalid user name (a-z, 0-9, _ or -, max 32, must start with a letter or _)")
	ErrExists   = errors.New("user already exists")
	ErrNotFound = errors.New("no such user")
	ErrShort    = fmt.Errorf("password too short (min %d)", MinPassword)
	ErrNoAuth   = errors.New("a user needs a password or at least one key")
)

type Key struct {
	Name        string    `json:"name"`
	Line        string    `json:"line"` // authorized_keys format
	Fingerprint string    `json:"fingerprint"`
	Added       time.Time `json:"added"`
}

type User struct {
	Name     string    `json:"name"`
	Hash     string    `json:"hash,omitempty"` // argon2id, "" = no password
	Keys     []Key     `json:"keys,omitempty"`
	Approved []string  `json:"approved,omitempty"` // key fingerprints the owner approved (R9)
	Created  time.Time `json:"created"`
	Disabled bool      `json:"disabled,omitempty"`
	Removed  bool      `json:"removed,omitempty"` // removed without --purge: space kept, login refused
	Disk     string    `json:"disk,omitempty"`
	LastSeen time.Time `json:"last_seen,omitempty"`
}

type Store struct {
	Users map[string]*User `json:"users"`
}

var mu sync.Mutex

// Load reads users.json (missing file = no users).
func Load() *Store {
	s := &Store{}
	config.ReadJSON(File, s)
	if s.Users == nil {
		s.Users = map[string]*User{}
	}
	return s
}

// Update loads, applies f and saves under one lock, so CLI and agent never lose writes.
func Update(f func(s *Store) error) error {
	mu.Lock()
	defer mu.Unlock()
	s := Load()
	if err := f(s); err != nil {
		return err
	}
	return config.WriteJSON(File, s)
}

func ValidName(n string) bool { return nameRe.MatchString(n) && !reserved[n] }

func (s *Store) Get(name string) (*User, bool) {
	u, ok := s.Users[name]
	return u, ok
}

func (s *Store) Names() []string {
	var n []string
	for k := range s.Users {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}

// Add creates a user. password may be empty when keys are given.
func (s *Store) Add(name, password string, keys []string, disk string) error {
	if !ValidName(name) {
		return ErrName
	}
	if _, ok := s.Users[name]; ok {
		return ErrExists
	}
	if password == "" && len(keys) == 0 {
		return ErrNoAuth
	}
	u := &User{Name: name, Created: time.Now().UTC(), Disk: disk}
	if password != "" {
		if err := u.SetPassword(password); err != nil {
			return err
		}
	}
	for i, k := range keys {
		if err := u.AddKey(fmt.Sprintf("key%d", i+1), k); err != nil {
			return err
		}
	}
	s.Users[name] = u
	return nil
}

// argon2id parameters (RFC 9106 second recommendation, lighter memory for small nodes).
const (
	aTime    = 3
	aMemory  = 64 * 1024
	aThreads = 2
	aLen     = 32
)

func (u *User) SetPassword(p string) error {
	if len([]rune(p)) < MinPassword {
		return ErrShort
	}
	salt := make([]byte, 16)
	rand.Read(salt)
	h := argon2.IDKey([]byte(p), salt, aTime, aMemory, aThreads, aLen)
	e := base64.RawStdEncoding
	u.Hash = fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", aMemory, aTime, aThreads, e.EncodeToString(salt), e.EncodeToString(h))
	return nil
}

// CheckPassword compares in constant time; a user without a password never matches.
func (u *User) CheckPassword(p string) bool {
	f := strings.Split(u.Hash, "$")
	if len(f) != 6 || f[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var par uint8
	if _, err := fmt.Sscanf(f[3], "m=%d,t=%d,p=%d", &m, &t, &par); err != nil {
		return false
	}
	e := base64.RawStdEncoding
	salt, err1 := e.DecodeString(f[4])
	want, err2 := e.DecodeString(f[5])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(p), salt, t, m, par, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func (u *User) AddKey(name, line string) error {
	pk, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(line)))
	if err != nil {
		return errors.New("invalid public key")
	}
	fp := ssh.FingerprintSHA256(pk)
	for _, k := range u.Keys {
		if k.Fingerprint == fp {
			return nil
		}
	}
	if name == "" {
		name = comment
	}
	if name == "" {
		name = fmt.Sprintf("key%d", len(u.Keys)+1)
	}
	u.Keys = append(u.Keys, Key{Name: name, Line: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pk))), Fingerprint: fp, Added: time.Now().UTC()})
	return nil
}

// RemoveKey removes by name or fingerprint and reports whether one was removed.
func (u *User) RemoveKey(id string) bool {
	out := u.Keys[:0]
	found := false
	for _, k := range u.Keys {
		if k.Name == id || k.Fingerprint == id {
			found = true
			continue
		}
		out = append(out, k)
	}
	u.Keys = out
	return found
}

// HasKey reports whether pk is one of the user's keys.
func (u *User) HasKey(pk ssh.PublicKey) bool {
	fp := ssh.FingerprintSHA256(pk)
	for _, k := range u.Keys {
		if k.Fingerprint == fp {
			return true
		}
	}
	return false
}

func (u *User) KeyApproved(fp string) bool {
	for _, a := range u.Approved {
		if a == fp {
			return true
		}
	}
	return false
}

func (u *User) ApproveKey(fp string) {
	if !u.KeyApproved(fp) {
		u.Approved = append(u.Approved, fp)
	}
}

// CanLogin is false for disabled or removed users.
func (u *User) CanLogin() bool { return !u.Disabled && !u.Removed }

// Instance is the user's Incus container name (R5).
func Instance(node, user string) string { return "cs-" + node + "-" + user }
