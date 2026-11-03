package proto

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// Fingerprint returns the OpenSSH "SHA256:..." fingerprint of an authorized_keys-style public key line.
func Fingerprint(hostKey string) (string, error) {
	f := strings.Fields(hostKey)
	if len(f) < 2 {
		return "", errors.New("invalid public key line")
	}
	raw, err := base64.StdEncoding.DecodeString(f[1])
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]), nil
}
