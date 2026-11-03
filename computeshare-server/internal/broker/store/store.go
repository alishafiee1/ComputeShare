// Package store is the broker's SQLite state (data-model.md). It never stores
// tokens in clear, passwords, authorized_keys, inner-SSH usernames or content.
package store

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alishafiee1/computeshare-client/proto"
	"golang.org/x/crypto/argon2"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

var (
	ErrNameTaken    = errors.New(proto.CodeNameTaken)
	ErrTokenInvalid = errors.New(proto.CodeTokenInvalid)
	ErrBanned       = errors.New(proto.CodeNodeBanned)
	ErrNotFound     = errors.New("not found")
)

type Store struct {
	db  *sql.DB
	mu  sync.Mutex // serialises registration (port allocation + unique name)
	Now func() time.Time
	dir string // state directory (the database folder); holds the 2FA encryption key
	// RequireApproval puts newly registered nodes in status "pending" until an admin approves them.
	RequireApproval bool
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	if err := migrate(db); err != nil {
		return nil, err
	}
	if _, err := db.Exec(twofaSchema); err != nil {
		return nil, fmt.Errorf("2fa schema: %w", err)
	}
	return &Store{db: db, Now: time.Now, dir: filepath.Dir(path)}, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) now() int64   { return s.Now().Unix() }

func randID(prefix string, n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func hashToken(secret, salt []byte) []byte {
	return argon2.IDKey(secret, salt, 1, 64*1024, 2, 32)
}

// TokenCreate returns the clear token "cs1.<id>.<secret>" exactly once; only an argon2id hash is stored (R2).
func (s *Store) TokenCreate(label string, ttl time.Duration, uses int) (id, token string, err error) {
	id = randID("t", 4)
	sec := make([]byte, 24)
	rand.Read(sec)
	secret := base64.RawURLEncoding.EncodeToString(sec)
	salt := make([]byte, 16)
	rand.Read(salt)
	var exp, mu any
	if ttl > 0 {
		exp = s.Now().Add(ttl).Unix()
	}
	if uses > 0 {
		mu = uses
	}
	_, err = s.db.Exec(`INSERT INTO join_tokens(id,token_hash,salt,label,expires_at,max_uses,created_at) VALUES(?,?,?,?,?,?,?)`,
		id, hashToken([]byte(secret), salt), salt, label, exp, mu, s.now())
	return id, "cs1." + id + "." + secret, err
}

type TokenRow struct {
	ID, Label                   string
	ExpiresAt, MaxUses, Revoked sql.NullInt64
	Uses                        int
	CreatedAt                   int64
}

func (s *Store) TokenList() ([]TokenRow, error) {
	rows, err := s.db.Query(`SELECT id,label,expires_at,max_uses,uses,revoked_at,created_at FROM join_tokens ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TokenRow
	for rows.Next() {
		var t TokenRow
		rows.Scan(&t.ID, &t.Label, &t.ExpiresAt, &t.MaxUses, &t.Uses, &t.Revoked, &t.CreatedAt)
		out = append(out, t)
	}
	return out, nil
}

func (s *Store) TokenRevoke(id string) error {
	r, err := s.db.Exec(`UPDATE join_tokens SET revoked_at=? WHERE id=? OR label=?`, s.now(), id, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// checkToken validates and consumes one use inside tx.
func (s *Store) checkToken(tx *sql.Tx, token string) (string, error) {
	p := strings.SplitN(token, ".", 3)
	if len(p) != 3 || p[0] != "cs1" {
		return "", ErrTokenInvalid
	}
	var hash, salt []byte
	var exp, mu, rev sql.NullInt64
	var uses int64
	err := tx.QueryRow(`SELECT token_hash,salt,expires_at,max_uses,uses,revoked_at FROM join_tokens WHERE id=?`, p[1]).Scan(&hash, &salt, &exp, &mu, &uses, &rev)
	if err != nil {
		return "", ErrTokenInvalid
	}
	if subtle.ConstantTimeCompare(hashToken([]byte(p[2]), salt), hash) != 1 || rev.Valid ||
		(exp.Valid && s.now() > exp.Int64) || (mu.Valid && uses >= mu.Int64) {
		return "", ErrTokenInvalid
	}
	_, err = tx.Exec(`UPDATE join_tokens SET uses=uses+1 WHERE id=?`, p[1])
	return p[1], err
}

type Node struct {
	ID, Name, DevicePubkey, HostKey, HostKeySig, Status, OS, Arch, Version string
	Specs                                                                  proto.Specs
	Limits                                                                 proto.Limits
	LastSeen, OnlineSince, RegisteredAt                                    int64
}

// NameAvailable reports whether a (case-insensitive) name is free.
func (s *Store) NameAvailable(name string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE name_key=?`, proto.NameKey(name)).Scan(&n)
	return n == 0, err
}

// Register implements protocol §2 after the caller validated name+signature (R3, R4).
func (s *Store) Register(r *proto.RegisterRequest) (*Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.IsBanned("node", proto.NameKey(r.Name)) || s.IsBanned("device", r.DevicePubkey) {
		return nil, ErrBanned
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var n int
	tx.QueryRow(`SELECT COUNT(*) FROM nodes WHERE name_key=?`, proto.NameKey(r.Name)).Scan(&n)
	if n > 0 {
		return nil, ErrNameTaken
	}
	tokID, err := s.checkToken(tx, r.Token)
	if err != nil {
		return nil, err
	}
	status := "offline"
	if s.RequireApproval {
		status = "pending"
	}
	nd := &Node{ID: randID("n_", 8), Name: r.Name, DevicePubkey: r.DevicePubkey, HostKey: r.HostKey, HostKeySig: r.HostKeySig,
		Status: status, OS: r.OS, Arch: r.Arch, Version: r.Version, Specs: r.Specs, Limits: r.Limits, RegisteredAt: r.RegisteredAt}
	sp, _ := json.Marshal(r.Specs)
	li, _ := json.Marshal(r.Limits)
	_, err = tx.Exec(`INSERT INTO nodes(id,name,name_key,device_pubkey,host_key,host_key_sig,status,os,arch,specs_json,limits_json,version,registered_at,token_id)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, nd.ID, nd.Name, proto.NameKey(nd.Name), nd.DevicePubkey, nd.HostKey, nd.HostKeySig, status,
		nd.OS, nd.Arch, string(sp), string(li), nd.Version, nd.RegisteredAt, tokID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrNameTaken
		}
		return nil, err
	}
	return nd, tx.Commit()
}

const nodeCols = `id,name,device_pubkey,host_key,host_key_sig,status,os,arch,version,specs_json,limits_json,last_seen,online_since,registered_at`

func scanNode(sc interface{ Scan(...any) error }) (*Node, error) {
	var n Node
	var sp, li, os, arch, ver sql.NullString
	if err := sc.Scan(&n.ID, &n.Name, &n.DevicePubkey, &n.HostKey, &n.HostKeySig, &n.Status, &os, &arch, &ver, &sp, &li, &n.LastSeen, &n.OnlineSince, &n.RegisteredAt); err != nil {
		return nil, err
	}
	n.OS, n.Arch, n.Version = os.String, arch.String, ver.String
	json.Unmarshal([]byte(sp.String), &n.Specs)
	json.Unmarshal([]byte(li.String), &n.Limits)
	return &n, nil
}

func (s *Store) NodeByID(id string) (*Node, error) {
	n, err := scanNode(s.db.QueryRow(`SELECT `+nodeCols+` FROM nodes WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return n, err
}

func (s *Store) NodeByName(name string) (*Node, error) {
	n, err := scanNode(s.db.QueryRow(`SELECT `+nodeCols+` FROM nodes WHERE name_key=?`, proto.NameKey(name)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return n, err
}

func (s *Store) Nodes() ([]*Node, error) {
	rows, err := s.db.Query(`SELECT ` + nodeCols + ` FROM nodes ORDER BY name_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func (s *Store) SetStatus(id, status string) error {
	now := s.now()
	if status == "online" {
		_, err := s.db.Exec(`UPDATE nodes SET status=?, last_seen=?, online_since=CASE WHEN status IN ('online','paused') THEN online_since ELSE ? END WHERE id=?`, status, now, now, id)
		return err
	}
	_, err := s.db.Exec(`UPDATE nodes SET status=?, last_seen=CASE WHEN ?='offline' THEN last_seen ELSE ? END WHERE id=?`, status, status, now, id)
	return err
}

func (s *Store) Touch(id string) error {
	_, err := s.db.Exec(`UPDATE nodes SET last_seen=? WHERE id=?`, s.now(), id)
	return err
}

func (s *Store) UpdateHostKey(id, hk, sig string) error {
	_, err := s.db.Exec(`UPDATE nodes SET host_key=?, host_key_sig=? WHERE id=?`, hk, sig, id)
	return err
}

// Release frees a name (signed Leave or admin) (R7).
func (s *Store) Release(id string) error {
	_, err := s.db.Exec(`DELETE FROM nodes WHERE id=?`, id)
	s.db.Exec(`DELETE FROM stats_samples WHERE node_id=?`, id)
	return err
}

// ---- stats (broker 003) ----

// AddSample stores at most one sample per node per minute (R2).
func (s *Store) AddSample(id string, st proto.Stats) (bool, error) {
	now := s.now()
	var last sql.NullInt64
	s.db.QueryRow(`SELECT MAX(ts) FROM stats_samples WHERE node_id=?`, id).Scan(&last)
	if last.Valid && now-last.Int64 < 60 {
		return false, nil
	}
	_, err := s.db.Exec(`INSERT INTO stats_samples VALUES(?,?,?,?,?,?,?,?)`, id, now, st.CPUPct, st.RAMPct, st.GPUPct, st.VRAMPct, st.RxBps, st.TxBps)
	return true, err
}

// Avg24h returns the mean CPU% of the last 24h, nil if < 10 samples (R3).
func (s *Store) Avg24h(id string) *float64 {
	var avg sql.NullFloat64
	var n int
	s.db.QueryRow(`SELECT AVG(cpu_pct), COUNT(*) FROM stats_samples WHERE node_id=? AND ts>=?`, id, s.now()-86400).Scan(&avg, &n)
	if n < 10 || !avg.Valid {
		return nil
	}
	return &avg.Float64
}

// ---- sessions (broker 002 R8, 004 R5) ----

func (s *Store) SessionStart(nodeID, via, src string) int64 {
	r, err := s.db.Exec(`INSERT INTO sessions(node_id,via,src_ip,started_at) VALUES(?,?,?,?)`, nodeID, via, src, s.now())
	if err != nil {
		return 0
	}
	id, _ := r.LastInsertId()
	return id
}

func (s *Store) SessionEnd(id, up, down int64) {
	s.db.Exec(`UPDATE sessions SET ended_at=?, bytes_up=?, bytes_down=? WHERE id=?`, s.now(), up, down, id)
}

type Session struct {
	ID                           int64
	NodeID, Via, SrcIP           string
	StartedAt, EndedAt, Up, Down int64
}

func (s *Store) Sessions() ([]Session, error) {
	rows, err := s.db.Query(`SELECT id,node_id,via,src_ip,started_at,COALESCE(ended_at,0),bytes_up,bytes_down FROM sessions ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var x Session
		rows.Scan(&x.ID, &x.NodeID, &x.Via, &x.SrcIP, &x.StartedAt, &x.EndedAt, &x.Up, &x.Down)
		out = append(out, x)
	}
	return out, nil
}

// Prune deletes stats > 7 days and sessions > 30 days, and expired bans.
func (s *Store) Prune() {
	now := s.now()
	s.db.Exec(`DELETE FROM stats_samples WHERE ts < ?`, now-7*86400)
	s.db.Exec(`DELETE FROM sessions WHERE started_at < ?`, now-30*86400)
	s.db.Exec(`DELETE FROM bans WHERE until IS NOT NULL AND until < ?`, now)
	s.db.Exec(`DELETE FROM panel_sessions WHERE expires_at < ?`, now)
}

// ---- bans (broker 004) ----

func (s *Store) Ban(kind, value string, d time.Duration, reason string) error {
	var until any
	if d > 0 {
		until = s.Now().Add(d).Unix()
	}
	_, err := s.db.Exec(`INSERT INTO bans(kind,value,until,reason,created_at) VALUES(?,?,?,?,?)
		ON CONFLICT(kind,value) DO UPDATE SET until=excluded.until, reason=excluded.reason`, kind, value, until, reason, s.now())
	return err
}

func (s *Store) Unban(kind, value string) error {
	_, err := s.db.Exec(`DELETE FROM bans WHERE kind=? AND value=?`, kind, value)
	return err
}

func (s *Store) IsBanned(kind, value string) bool {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM bans WHERE kind=? AND value=? AND (until IS NULL OR until>=?)`, kind, value, s.now()).Scan(&n)
	return n > 0
}

// SetNodeBanned marks status banned (admin) or clears it.
func (s *Store) SetNodeBanned(id string) error {
	_, err := s.db.Exec(`UPDATE nodes SET status='banned' WHERE id=?`, id)
	return err
}
