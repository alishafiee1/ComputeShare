// Package control holds owner actions shared by the csnode CLI and the local web panel
// (client card 007): share on/off and resource limits.
package control

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/alishafiee1/computeshare-client/internal/node/config"
	"github.com/alishafiee1/computeshare-client/internal/node/incus"
	"github.com/alishafiee1/computeshare-client/internal/node/setup"
	"github.com/alishafiee1/computeshare-client/proto"
)

// Limits is a partial update: nil = unchanged, "" = unlimited.
type Limits struct {
	CPU, Memory, Bandwidth, Quota *string
}

var memRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?(MiB|GiB|TiB|MB|GB|TB)$`)

// Validate checks every set field and returns the first error in plain words.
func (l Limits) Validate() error {
	if l.CPU != nil && *l.CPU != "" {
		n, err := strconv.Atoi(*l.CPU)
		if err != nil || n < 1 || n > 1024 {
			return fmt.Errorf("cpu: whole number of cores (e.g. 4) or empty for unlimited")
		}
	}
	if l.Memory != nil && *l.Memory != "" && !memRe.MatchString(*l.Memory) {
		return fmt.Errorf("memory: size like 8GiB or 4096MiB, or empty for unlimited")
	}
	if l.Bandwidth != nil {
		if _, err := proto.ParseBandwidth(*l.Bandwidth); err != nil {
			return fmt.Errorf("bandwidth: %v", err)
		}
	}
	if l.Quota != nil {
		if _, err := proto.ParseQuota(*l.Quota); err != nil {
			return fmt.Errorf("quota: %v", err)
		}
	}
	return nil
}

// Applier changes the running space; replaced in tests.
type Applier interface {
	SetCPU(inst, v string) error
	SetMemory(inst, v string) error
	SetBandwidth(name, v string) error
}

type incusApplier struct{}

func setOrUnset(inst, key, v string) error {
	if v == "" {
		_, err := incus.Run(nil, "config", "unset", inst, key)
		return err
	}
	return incus.SetConfig(inst, key+"="+v)
}
func (incusApplier) SetCPU(inst, v string) error       { return setOrUnset(inst, "limits.cpu", v) }
func (incusApplier) SetMemory(inst, v string) error    { return setOrUnset(inst, "limits.memory", v) }
func (incusApplier) SetBandwidth(name, v string) error { return setup.SetBandwidth(name, v) }

// Default applies through Incus when the space exists.
var Default Applier = incusApplier{}

// ApplyLimits validates, applies to the space (if it exists) and saves config.yaml.
func ApplyLimits(c *config.Config, l Limits, a Applier, spaceExists bool) error {
	for _, p := range []*string{l.CPU, l.Memory, l.Bandwidth, l.Quota} {
		if p != nil {
			v := strings.TrimSpace(*p)
			if strings.EqualFold(v, "off") || strings.EqualFold(v, "unlimited") {
				v = ""
			}
			*p = v
		}
	}
	if err := l.Validate(); err != nil {
		return err
	}
	inst := c.Space.Instance
	if l.CPU != nil {
		if spaceExists {
			if err := a.SetCPU(inst, *l.CPU); err != nil {
				return err
			}
		}
		c.Limits.CPU = *l.CPU
	}
	if l.Memory != nil {
		if spaceExists {
			if err := a.SetMemory(inst, *l.Memory); err != nil {
				return err
			}
		}
		c.Limits.Memory = *l.Memory
	}
	if l.Bandwidth != nil {
		if spaceExists {
			if err := a.SetBandwidth(c.Name, *l.Bandwidth); err != nil {
				return err
			}
		}
		c.Limits.Bandwidth = *l.Bandwidth
	}
	if l.Quota != nil {
		c.Limits.Quota = *l.Quota // enforced by the agent's quota loop against today's counter
	}
	return c.Save()
}

// SetShare turns sharing on or off. Off = paused: the tunnel stays up (the node keeps
// showing as paused) but every new connection is refused.
func SetShare(on bool) error {
	st := config.LoadState()
	st.Paused = !on
	return st.Save()
}

// Sharing reports whether new connections are accepted.
func Sharing() bool { s := config.LoadState(); return !s.Paused && !s.Stopped }
