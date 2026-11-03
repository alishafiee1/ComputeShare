package setup

import (
	"golang.org/x/crypto/ssh"
)

func parseAuthorized(k string) (ssh.PublicKey, string, []string, []byte, error) {
	return ssh.ParseAuthorizedKey([]byte(k))
}

// KeyFingerprint returns SHA256:... of an authorized_keys line.
func KeyFingerprint(k string) (string, error) {
	pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(k))
	if err != nil {
		return "", err
	}
	return ssh.FingerprintSHA256(pk), nil
}
