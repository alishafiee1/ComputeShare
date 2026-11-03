package panel

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/alishafiee1/computeshare-client/proto"
	"golang.org/x/crypto/ssh"
)

func testReq(t *testing.T, name, tok string) *proto.RegisterRequest {
	dpub, dpriv, _ := ed25519.GenerateKey(rand.Reader)
	hpub, _, _ := ed25519.GenerateKey(rand.Reader)
	sp, _ := ssh.NewPublicKey(hpub)
	hk := string(ssh.MarshalAuthorizedKey(sp))
	ts := time.Now().Unix()
	return &proto.RegisterRequest{Proto: proto.Version, Name: name, Token: tok, DevicePubkey: proto.EncodePubkey(dpub),
		HostKey: hk, HostKeySig: proto.Sign(dpriv, proto.RegistrationBytes(name, hk, ts)), RegisteredAt: ts, OS: "linux", Arch: "amd64"}
}
