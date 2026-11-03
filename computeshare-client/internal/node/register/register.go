// Package register is the client side of protocol §2 (node 006 R1, R2).
package register

import (
	"bytes"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/alishafiee1/computeshare-client/proto"
)

// ErrUnreachable maps to exit code 3.
var ErrUnreachable = errors.New("broker unreachable")

type APIError struct {
	Code, Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Code + ": " + e.Message
	}
	return e.Code
}

func HTTPClient(caFile string) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		if pem, err := os.ReadFile(caFile); err == nil {
			pool := x509.NewCertPool()
			pool.AppendCertsFromPEM(pem)
			tr.TLSClientConfig.RootCAs = pool
		}
	}
	return &http.Client{Transport: tr, Timeout: 20 * time.Second}
}

func base(server string) string { return strings.TrimRight(server, "/") }

// NameAvailable implements the live name check (R1).
func NameAvailable(c *http.Client, server, name string) (bool, error) {
	resp, err := c.Get(base(server) + "/v1/names/" + url.PathEscape(name))
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	var r struct {
		Available bool   `json:"available"`
		Code      string `json:"code"`
	}
	json.NewDecoder(resp.Body).Decode(&r)
	if r.Code == proto.CodeNameInvalid {
		return false, &APIError{Code: proto.CodeNameInvalid, Message: "name must match ^[a-zA-Z][a-zA-Z0-9-]{2,31}$"}
	}
	if r.Code == proto.CodeNodeBanned {
		return false, &APIError{Code: proto.CodeNodeBanned, Message: "node banned"}
	}
	if resp.StatusCode != 200 {
		return false, &APIError{Code: r.Code, Message: resp.Status}
	}
	return r.Available, nil
}

// Build creates a signed RegisterRequest.
func Build(priv ed25519.PrivateKey, name, token, hostKey string, specs proto.Specs, lim proto.Limits, osName, arch, version string) proto.RegisterRequest {
	now := time.Now().Unix()
	return proto.RegisterRequest{Proto: proto.Version, Name: name, Token: token,
		DevicePubkey: proto.EncodePubkey(priv.Public().(ed25519.PublicKey)), HostKey: strings.TrimSpace(hostKey),
		HostKeySig: proto.Sign(priv, proto.RegistrationBytes(name, hostKey, now)), RegisteredAt: now,
		OS: osName, Arch: arch, Specs: specs, Limits: lim, Version: version}
}

func Register(c *http.Client, server string, req proto.RegisterRequest) (*proto.RegisterResponse, error) {
	b, _ := json.Marshal(req)
	resp, err := c.Post(base(server)+"/v1/register", "application/json", bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	var r proto.RegisterResponse
	json.NewDecoder(resp.Body).Decode(&r)
	if resp.StatusCode != 201 || r.Code != proto.CodeRegistered {
		if r.Code == proto.CodeNameTaken {
			r.Message = proto.NameTakenMessage(req.Name)
		}
		if r.Code == "" {
			r.Code = resp.Status
		}
		return nil, &APIError{Code: r.Code, Message: r.Message}
	}
	return &r, nil
}
