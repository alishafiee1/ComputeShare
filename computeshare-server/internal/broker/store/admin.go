package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/alishafiee1/computeshare-client/proto"
	"golang.org/x/crypto/bcrypt"
)

// ---- admin accounts, panel sessions, audit log, approvals (server 003) ----

var ErrBadLogin = errors.New("invalid username or password")

// AdminSetPassword creates or updates an admin. The caller validates password strength.
func (s *Store) AdminSetPassword(user, password string) error {
	h, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	now := s.now()
	_, err = s.db.Exec(`INSERT INTO admins(username,pw_hash,created_at,updated_at) VALUES(?,?,?,?)
		ON CONFLICT(username) DO UPDATE SET pw_hash=excluded.pw_hash, updated_at=excluded.updated_at`, user, h, now, now)
	if err == nil {
		// a password change logs out every session of that admin
		s.db.Exec(`DELETE FROM panel_sessions WHERE username=?`, user)
	}
	return err
}

// dummyHash keeps the timing of unknown-user logins close to real ones.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("computeshare-dummy-password"), 12)

// AdminCheck verifies credentials in roughly constant time.
func (s *Store) AdminCheck(user, password string) error {
	var h []byte
	err := s.db.QueryRow(`SELECT pw_hash FROM admins WHERE username=?`, user).Scan(&h)
	if err != nil {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return ErrBadLogin
	}
	if bcrypt.CompareHashAndPassword(h, []byte(password)) != nil {
		return ErrBadLogin
	}
	return nil
}

func (s *Store) AdminCount() int {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM admins`).Scan(&n)
	return n
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func sidHash(sid string) []byte { h := sha256.Sum256([]byte(sid)); return h[:] }

// PanelSession is a logged-in admin session. Only a hash of the session id is stored.
type PanelSession struct {
	Username, CSRF, IP   string
	CreatedAt, ExpiresAt int64
}

// PanelSessionCreate returns a new random session id (cookie value) and CSRF token.
func (s *Store) PanelSessionCreate(user, ip string, ttl time.Duration) (sid, csrf string, err error) {
	sid, csrf = randHex(32), randHex(16)
	now := s.now()
	_, err = s.db.Exec(`INSERT INTO panel_sessions(id_hash,username,csrf,ip,created_at,expires_at) VALUES(?,?,?,?,?,?)`,
		sidHash(sid), user, csrf, ip, now, now+int64(ttl/time.Second))
	return
}

func (s *Store) PanelSessionGet(sid string) (*PanelSession, error) {
	var p PanelSession
	err := s.db.QueryRow(`SELECT username,csrf,ip,created_at,expires_at FROM panel_sessions WHERE id_hash=?`, sidHash(sid)).
		Scan(&p.Username, &p.CSRF, &p.IP, &p.CreatedAt, &p.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && p.ExpiresAt < s.now()) {
		return nil, ErrNotFound
	}
	return &p, err
}

func (s *Store) PanelSessionDelete(sid string) {
	s.db.Exec(`DELETE FROM panel_sessions WHERE id_hash=?`, sidHash(sid))
}

// AuditEntry is one row of the admin audit log.
type AuditEntry struct {
	ID                        int64
	TS                        int64
	Actor, IP, Action, Detail string
}

// Audit appends an entry. Never pass secrets in detail.
func (s *Store) Audit(actor, ip, action, detail string) {
	s.db.Exec(`INSERT INTO audit(ts,actor,ip,action,detail) VALUES(?,?,?,?,?)`, s.now(), actor, ip, action, detail)
}

func (s *Store) AuditList(limit int) ([]AuditEntry, error) {
	rows, err := s.db.Query(`SELECT id,ts,actor,ip,action,detail FROM audit ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var a AuditEntry
		rows.Scan(&a.ID, &a.TS, &a.Actor, &a.IP, &a.Action, &a.Detail)
		out = append(out, a)
	}
	return out, nil
}

// SessionLog is one connection-log row joined with the node name.
type SessionLog struct {
	Session
	Node string
}

// RecentSessions returns the newest connection-log rows (metadata only, never content).
func (s *Store) RecentSessions(limit int) ([]SessionLog, error) {
	rows, err := s.db.Query(`SELECT s.id,s.node_id,s.via,s.src_ip,s.started_at,COALESCE(s.ended_at,0),s.bytes_up,s.bytes_down,COALESCE(n.name,s.node_id)
		FROM sessions s LEFT JOIN nodes n ON n.id=s.node_id ORDER BY s.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionLog
	for rows.Next() {
		var x SessionLog
		rows.Scan(&x.ID, &x.NodeID, &x.Via, &x.SrcIP, &x.StartedAt, &x.EndedAt, &x.Up, &x.Down, &x.Node)
		out = append(out, x)
	}
	return out, nil
}

// Approve moves a pending node to offline (it may now open its tunnel).
func (s *Store) Approve(id string) error {
	r, err := s.db.Exec(`UPDATE nodes SET status='offline' WHERE id=? AND status='pending'`, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ClearNodeBanned clears an admin ban on a node row (status back to offline).
func (s *Store) ClearNodeBanned(id string) error {
	_, err := s.db.Exec(`UPDATE nodes SET status='offline' WHERE id=? AND status='banned'`, id)
	return err
}

// BanNode bans a node by name key and device key and marks the row banned (broker 004).
func (s *Store) BanNode(n *Node, reason string) {
	s.Ban("node", proto.NameKey(n.Name), 0, reason)
	s.Ban("device", n.DevicePubkey, 0, reason)
	s.SetNodeBanned(n.ID)
}

// UnbanNode lifts the bans of a node name (and its device key if the row still exists).
func (s *Store) UnbanNode(name string) {
	s.Unban("node", proto.NameKey(name))
	if n, err := s.NodeByName(name); err == nil {
		s.Unban("device", n.DevicePubkey)
		s.ClearNodeBanned(n.ID)
	}
}
