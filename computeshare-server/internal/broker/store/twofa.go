package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Two-step login for admins (server card 007): the TOTP secret is stored encrypted with a key
// kept in the state directory (file 2fa.key, mode 0600); backup codes are stored as bcrypt hashes.

var ErrBad2FA = errors.New("invalid code")

const backupCodes = 10

const twofaSchema = `
CREATE TABLE IF NOT EXISTS admin_2fa (
  username TEXT PRIMARY KEY, secret_enc BLOB NOT NULL, enabled INTEGER NOT NULL DEFAULT 0,
  last_step INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS admin_backup (
  id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL, code_hash BLOB NOT NULL, used INTEGER NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS admin_backup_user ON admin_backup(username);`

// keyFile is where the encryption key lives: next to the database, never inside it.
func (s *Store) keyFile() string { return filepath.Join(s.dir, "2fa.key") }

// key returns the 32-byte key, creating the file on first use.
func (s *Store) key() ([]byte, error) {
	if s.dir == "" {
		return nil, errors.New("2fa needs a state directory")
	}
	if b, err := os.ReadFile(s.keyFile()); err == nil {
		if len(b) != 32 {
			return nil, fmt.Errorf("%s has the wrong size; refusing to use it", s.keyFile())
		}
		return b, nil
	}
	b := make([]byte, 32)
	rand.Read(b)
	if err := os.WriteFile(s.keyFile(), b, 0o600); err != nil {
		return nil, err
	}
	os.Chmod(s.keyFile(), 0o600)
	return b, nil
}

func (s *Store) seal(plain []byte) ([]byte, error) {
	k, err := s.key()
	if err != nil {
		return nil, err
	}
	blk, _ := aes.NewCipher(k)
	g, _ := cipher.NewGCM(blk)
	n := make([]byte, g.NonceSize())
	rand.Read(n)
	return g.Seal(n, n, plain, nil), nil
}

func (s *Store) open(box []byte) ([]byte, error) {
	k, err := s.key()
	if err != nil {
		return nil, err
	}
	blk, _ := aes.NewCipher(k)
	g, _ := cipher.NewGCM(blk)
	if len(box) < g.NonceSize() {
		return nil, errors.New("corrupt secret")
	}
	return g.Open(nil, box[:g.NonceSize()], box[g.NonceSize():], nil)
}

func (s *Store) adminExists(user string) bool {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM admins WHERE username=?`, user).Scan(&n)
	return n > 0
}

// Admin2FAStart creates a pending secret for user (replacing an unconfirmed or old one) and returns it.
// Two-step login stays off until Admin2FAConfirm gets a valid code.
func (s *Store) Admin2FAStart(user string) (secret []byte, err error) {
	if !s.adminExists(user) {
		return nil, fmt.Errorf("no admin named %q (create it first with: csbroker admin set-password --user %s)", user, user)
	}
	secret = NewTOTPSecret()
	box, err := s.seal(secret)
	if err != nil {
		return nil, err
	}
	_, err = s.db.Exec(`INSERT INTO admin_2fa(username,secret_enc,enabled,last_step,created_at) VALUES(?,?,0,0,?)
		ON CONFLICT(username) DO UPDATE SET secret_enc=excluded.secret_enc, enabled=0, last_step=0, created_at=excluded.created_at`, user, box, s.now())
	return secret, err
}

func (s *Store) secretOf(user string) (secret []byte, enabled bool, last int64, err error) {
	var box []byte
	var en int
	if err = s.db.QueryRow(`SELECT secret_enc,enabled,last_step FROM admin_2fa WHERE username=?`, user).Scan(&box, &en, &last); err != nil {
		return nil, false, 0, err
	}
	secret, err = s.open(box)
	return secret, en == 1, last, err
}

// Admin2FAConfirm turns two-step login on when code is valid for the pending secret, replaces the
// backup codes with ten new ones (returned once, stored only as hashes) and ends the admin's sessions.
func (s *Store) Admin2FAConfirm(user, code string) (codes []string, err error) {
	secret, _, _, err := s.secretOf(user)
	if err != nil {
		return nil, errors.New("run `csbroker admin 2fa enable` first")
	}
	step, ok := verifyTOTP(secret, code, s.Now(), 0)
	if !ok {
		return nil, ErrBad2FA
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tx.Exec(`DELETE FROM admin_backup WHERE username=?`, user)
	for i := 0; i < backupCodes; i++ {
		c := newBackupCode()
		h, err := bcrypt.GenerateFromPassword([]byte(normBackup(c)), 10)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`INSERT INTO admin_backup(username,code_hash) VALUES(?,?)`, user, h); err != nil {
			return nil, err
		}
		codes = append(codes, c)
	}
	if _, err := tx.Exec(`UPDATE admin_2fa SET enabled=1, last_step=? WHERE username=?`, step, user); err != nil {
		return nil, err
	}
	tx.Exec(`DELETE FROM panel_sessions WHERE username=?`, user)
	return codes, tx.Commit()
}

// Admin2FAEnabled reports whether user must pass a second step to sign in.
func (s *Store) Admin2FAEnabled(user string) bool {
	_, en, _, err := s.secretOf(user)
	return err == nil && en
}

// Admin2FAVerify checks a TOTP code (each time step works once) or an unused backup code.
func (s *Store) Admin2FAVerify(user, code string) error {
	secret, en, last, err := s.secretOf(user)
	if err != nil || !en {
		return ErrBad2FA
	}
	if step, ok := verifyTOTP(secret, code, s.Now(), last); ok {
		r, err := s.db.Exec(`UPDATE admin_2fa SET last_step=? WHERE username=? AND last_step<?`, step, user, step)
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 1 {
			return nil
		}
		return ErrBad2FA // another request used this step first
	}
	return s.useBackup(user, code)
}

func (s *Store) useBackup(user, code string) error {
	c := normBackup(code)
	if len(c) != 10 {
		return ErrBad2FA
	}
	rows, err := s.db.Query(`SELECT id,code_hash FROM admin_backup WHERE username=? AND used=0`, user)
	if err != nil {
		return err
	}
	type row struct {
		id int64
		h  []byte
	}
	var all []row
	for rows.Next() {
		var r row
		rows.Scan(&r.id, &r.h)
		all = append(all, r)
	}
	rows.Close()
	for _, r := range all {
		if bcrypt.CompareHashAndPassword(r.h, []byte(c)) == nil {
			res, err := s.db.Exec(`UPDATE admin_backup SET used=1 WHERE id=? AND used=0`, r.id)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 1 {
				return nil
			}
		}
	}
	return ErrBad2FA
}

// Admin2FARemaining is how many unused backup codes user still has.
func (s *Store) Admin2FARemaining(user string) int {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM admin_backup WHERE username=? AND used=0`, user).Scan(&n)
	return n
}

// Admin2FADisable removes two-step login (local CLI only) and ends the admin's sessions.
func (s *Store) Admin2FADisable(user string) error {
	s.db.Exec(`DELETE FROM admin_backup WHERE username=?`, user)
	r, err := s.db.Exec(`DELETE FROM admin_2fa WHERE username=?`, user)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	s.db.Exec(`DELETE FROM panel_sessions WHERE username=?`, user)
	return nil
}

const backupAlphabet = "abcdefghjkmnpqrstuvwxyz23456789" // no look-alike letters

// newBackupCode makes a code like "k3m9p-x7bqa" (10 characters, about 50 bits).
func newBackupCode() string {
	b := make([]byte, 10)
	rand.Read(b)
	out := make([]byte, 10)
	for i, v := range b {
		out[i] = backupAlphabet[int(v)%len(backupAlphabet)]
	}
	return string(out[:5]) + "-" + string(out[5:])
}

func normBackup(c string) string {
	c = strings.ToLower(strings.TrimSpace(c))
	c = strings.ReplaceAll(c, "-", "")
	return strings.ReplaceAll(c, " ", "")
}

var _ = time.Second
