package proto

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
)

// Canonical signing bytes (R2). "‖" is implemented as a single 0x0A (LF)
// separator, which cannot occur inside a valid name, node id, decimal
// timestamp or single-line OpenSSH public key.
const sep = "\n"

func RegistrationBytes(name, hostKey string, registeredAt int64) []byte {
	return []byte("cs-reg-v1" + sep + strings.ToLower(name) + sep + strings.TrimSpace(hostKey) + sep + strconv.FormatInt(registeredAt, 10))
}

func HelloBytes(nodeID string, ts int64) []byte {
	return []byte("cs-hello-v1" + sep + nodeID + sep + strconv.FormatInt(ts, 10))
}

func LeaveBytes(nodeID string, ts int64) []byte {
	return []byte("cs-leave-v1" + sep + nodeID + sep + strconv.FormatInt(ts, 10))
}

const pubPrefix = "ed25519:"

// EncodePubkey renders "ed25519:<base64>".
func EncodePubkey(pub ed25519.PublicKey) string {
	return pubPrefix + base64.StdEncoding.EncodeToString(pub)
}

// DecodePubkey parses "ed25519:<base64>".
func DecodePubkey(s string) (ed25519.PublicKey, error) {
	if !strings.HasPrefix(s, pubPrefix) {
		return nil, errors.New("device_pubkey must start with ed25519:")
	}
	b, err := base64.StdEncoding.DecodeString(s[len(pubPrefix):])
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("invalid ed25519 public key")
	}
	return ed25519.PublicKey(b), nil
}

// Sign returns base64(ed25519 signature).
func Sign(priv ed25519.PrivateKey, msg []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, msg))
}

// Verify checks a base64 signature made by Sign against an encoded pubkey.
func Verify(pubkey string, msg []byte, sig string) bool {
	pub, err := DecodePubkey(pubkey)
	if err != nil {
		return false
	}
	s, err := base64.StdEncoding.DecodeString(sig)
	if err != nil || len(s) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(pub, msg, s)
}

// SkewOK reports whether |now-ts| <= MaxClockSkew.
func SkewOK(now, ts int64) bool {
	d := now - ts
	if d < 0 {
		d = -d
	}
	return d <= MaxClockSkew
}
