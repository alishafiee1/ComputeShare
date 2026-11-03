package server

import (
	"net/http"
	"time"

	"github.com/alishafiee1/computeshare-client/proto"
	"github.com/alishafiee1/computeshare-server/internal/broker/panel"
)

// AdminNodes lists every node (pending and banned included) for the admin panel.
func (s *Server) AdminNodes() []panel.NodeView {
	nodes, _ := s.St.Nodes()
	now := time.Now().Unix()
	out := make([]panel.NodeView, 0, len(nodes))
	for _, n := range nodes {
		v := panel.NodeView{ID: n.ID, Name: n.Name, Status: n.Status, OS: n.OS, Arch: n.Arch, Version: n.Version,
			Specs: n.Specs, Limits: n.Limits, LastSeen: n.LastSeen, RegisteredAt: n.RegisteredAt}
		v.HostKeyFP, _ = proto.Fingerprint(n.HostKey)
		if c := s.Hub.Get(n.ID); c != nil && n.Status != "banned" {
			v.Status = "online"
			if c.Paused() {
				v.Status = "paused"
			}
			v.LastSeen = c.LastPing.Load()
			v.UptimeS = now - n.OnlineSince
			if st := c.Stats(); st != nil {
				cpu, ram := st.CPUPct, st.RAMPct
				v.LoadNow, v.RAMPct, v.GPUPct = &cpu, &ram, st.GPUPct
			}
		} else if n.Status == "online" || n.Status == "paused" {
			v.Status = "offline"
		}
		v.Avg24h = s.St.Avg24h(n.ID)
		out = append(out, v)
	}
	return out
}

// Disconnect closes a node's tunnel now (the node reconnects unless banned or removed).
func (s *Server) Disconnect(nodeID string) bool {
	c := s.Hub.Get(nodeID)
	if c == nil {
		return false
	}
	c.Close()
	return true
}

// SetPanel mounts the admin panel at /panel/ (may be called before or after Run).
func (s *Server) SetPanel(p *panel.Panel) { s.panel.Store(http.Handler(p)) }
