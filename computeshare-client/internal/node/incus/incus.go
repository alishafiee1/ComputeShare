// Package incus is a thin wrapper over the `incus` CLI. It only ever touches
// instances named cs-* and the csbr0 network / csacl ACL (node 001 edge case).
package incus

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var Bin = "incus"

// Run executes incus with args; stdin may be nil.
func Run(stdin []byte, args ...string) (string, error) {
	cmd := exec.Command(Bin, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("incus %s: %v: %s", strings.Join(redact(args), " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

func redact(a []string) []string {
	out := append([]string(nil), a...)
	for i, s := range out {
		if strings.Contains(s, "chpasswd") || strings.Contains(s, "password") {
			out[i] = "[REDACTED]"
		}
	}
	return out
}

func guard(instance string) error {
	if !strings.HasPrefix(instance, "cs-") {
		return errors.New("refusing to touch non cs-* instance " + instance)
	}
	return nil
}

func Available() bool {
	_, err := exec.LookPath(Bin)
	if err != nil {
		return false
	}
	_, err = Run(nil, "info")
	return err == nil
}

func Exec(instance string, stdin []byte, cmd ...string) (string, error) {
	if err := guard(instance); err != nil {
		return "", err
	}
	return Run(stdin, append([]string{"exec", instance, "--"}, cmd...)...)
}

type State struct {
	Status string `json:"status"`
	CPU    struct {
		Usage int64 `json:"usage"`
	} `json:"cpu"`
	Memory struct {
		Usage int64 `json:"usage"`
		Total int64 `json:"total"`
	} `json:"memory"`
	Network map[string]struct {
		Addresses []struct {
			Family  string `json:"family"`
			Address string `json:"address"`
			Scope   string `json:"scope"`
		} `json:"addresses"`
		Counters struct {
			BytesReceived int64 `json:"bytes_received"`
			BytesSent     int64 `json:"bytes_sent"`
		} `json:"counters"`
		HostName string `json:"host_name"`
	} `json:"network"`
	Disk map[string]struct {
		Usage int64 `json:"usage"`
	} `json:"disk"`
}

func GetState(instance string) (*State, error) {
	if err := guard(instance); err != nil {
		return nil, err
	}
	out, err := Run(nil, "query", "/1.0/instances/"+instance+"/state")
	if err != nil {
		return nil, err
	}
	var s State
	return &s, json.Unmarshal([]byte(out), &s)
}

// IPv4 returns the global IPv4 of eth0.
func IPv4(instance string) (string, error) {
	s, err := GetState(instance)
	if err != nil {
		return "", err
	}
	if n, ok := s.Network["eth0"]; ok {
		for _, a := range n.Addresses {
			if a.Family == "inet" && a.Scope == "global" {
				return a.Address, nil
			}
		}
	}
	return "", errors.New("space has no IPv4 yet")
}

func Exists(instance string) bool {
	_, err := Run(nil, "query", "/1.0/instances/"+instance)
	return err == nil
}

func SetConfig(instance string, kv ...string) error {
	if err := guard(instance); err != nil {
		return err
	}
	_, err := Run(nil, append([]string{"config", "set", instance}, kv...)...)
	return err
}

func Stop(instance string, force bool) error {
	if err := guard(instance); err != nil {
		return err
	}
	a := []string{"stop", instance}
	if force {
		a = append(a, "--force")
	}
	_, err := Run(nil, a...)
	return err
}

func Start(instance string) error {
	if err := guard(instance); err != nil {
		return err
	}
	_, err := Run(nil, "start", instance)
	return err
}

func Delete(instance string) error {
	if err := guard(instance); err != nil {
		return err
	}
	_, err := Run(nil, "delete", instance, "--force")
	return err
}

func PushFile(instance, path string, data []byte, mode string) error {
	if err := guard(instance); err != nil {
		return err
	}
	_, err := Run(data, "file", "push", "-", instance+path, "--mode", mode, "--uid", "0", "--gid", "0", "--create-dirs")
	return err
}

func PullFile(instance, path string) ([]byte, error) {
	if err := guard(instance); err != nil {
		return nil, err
	}
	out, err := Run(nil, "file", "pull", instance+path, "-")
	return []byte(out), err
}

// Host checks used by setup (R1/R2).
func OSRelease() map[string]string {
	m := map[string]string{}
	b, _ := os.ReadFile("/etc/os-release")
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			m[k] = strings.Trim(v, `"`)
		}
	}
	return m
}
