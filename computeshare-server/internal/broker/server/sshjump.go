package server

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/alishafiee1/computeshare-server/internal/broker/hub"
	"github.com/alishafiee1/computeshare-server/internal/broker/limits"
	"github.com/alishafiee1/computeshare-server/internal/broker/list"
	"github.com/alishafiee1/computeshare-server/internal/broker/store"
	"golang.org/x/crypto/ssh"
)

// serveSSH implements protocol §4.1 (jump user `join`) and §4.3 (user `list`).
// No shell, no exec, no global requests: the broker never runs commands.
func (s *Server) serveSSH(l net.Listener) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go s.handleSSH(c)
	}
}

func (s *Server) handleSSH(c net.Conn) {
	ip, _, _ := net.SplitHostPort(c.RemoteAddr().String())
	if !s.Lim.Allow(limits.SSH, ip) {
		s.Log.Warn("ssh connection refused: rate limit", "src_ip", ip)
		c.Close()
		return
	}
	c.SetDeadline(time.Now().Add(20 * time.Second))
	sc, chans, reqs, err := ssh.NewServerConn(c, s.sshCfg)
	if err != nil {
		s.Log.Info("ssh handshake failed", "src_ip", ip, "err", err.Error())
		s.Lim.Strike(limits.BadHandshake, ip)
		c.Close()
		return
	}
	c.SetDeadline(time.Time{})
	defer sc.Close()
	go func() {
		for r := range reqs {
			if r.WantReply {
				r.Reply(false, nil) // all global requests rejected (tcpip-forward etc.)
			}
		}
	}()
	user := sc.User()
	for nc := range chans {
		switch {
		case user == "join" && nc.ChannelType() == "direct-tcpip":
			go s.jump(nc, ip)
		case user == "list" && nc.ChannelType() == "session":
			go s.listSession(nc)
		default:
			nc.Reject(ssh.Prohibited, "administratively prohibited")
		}
	}
}

type directTCPIP struct {
	Host     string
	Port     uint32
	OrigHost string
	OrigPort uint32
}

func (s *Server) jump(nc ssh.NewChannel, ip string) {
	var d directTCPIP
	if err := ssh.Unmarshal(nc.ExtraData(), &d); err != nil {
		nc.Reject(ssh.ConnectionFailed, "CONNECT_FAILED")
		return
	}
	name := strings.TrimSuffix(strings.ToLower(d.Host), ".cs")
	if d.Port != 22 {
		nc.Reject(ssh.ConnectionFailed, "CONNECT_FAILED")
		return
	}
	nd, err := s.St.NodeByName(name)
	if errors.Is(err, store.ErrNotFound) {
		s.Lim.Strike(limits.UnknownNode, ip)
	}
	if err != nil {
		nc.Reject(ssh.ConnectionFailed, "CONNECT_FAILED")
		return
	}
	conn := s.Hub.Get(nd.ID)
	if conn == nil || nd.Status == "banned" {
		seen := "never"
		if nd.LastSeen > 0 {
			seen = hub.Age(time.Now().Unix() - nd.LastSeen)
		}
		nc.Reject(ssh.ConnectionFailed, fmt.Sprintf("%s is offline (last seen %s)", nd.Name, seen))
		return
	}
	if conn.Paused() {
		nc.Reject(ssh.ConnectionFailed, fmt.Sprintf("%s: node paused", nd.Name))
		return
	}
	ch, creqs, err := nc.Accept()
	if err != nil {
		return
	}
	go ssh.DiscardRequests(creqs)
	s.bridge(ch, nd.ID, nd.Name, "jump", ip)
}

// listSession answers exec/shell with generated text only (R6: no command execution).
func (s *Server) listSession(nc ssh.NewChannel) {
	ch, reqs, err := nc.Accept()
	if err != nil {
		return
	}
	defer ch.Close()
	for r := range reqs {
		switch r.Type {
		case "exec", "shell":
			arg := ""
			if r.Type == "exec" && len(r.Payload) >= 4 {
				n := binary.BigEndian.Uint32(r.Payload)
				if int(n) <= len(r.Payload)-4 {
					arg = strings.TrimSpace(string(r.Payload[4 : 4+n]))
				}
			}
			r.Reply(true, nil)
			out, code := s.listOutput(arg)
			ch.Write([]byte(out))
			ch.SendRequest("exit-status", false, ssh.Marshal(struct{ C uint32 }{code}))
			return
		case "pty-req", "env", "window-change":
			if r.WantReply {
				r.Reply(r.Type == "pty-req", nil)
			}
		default:
			if r.WantReply {
				r.Reply(false, nil)
			}
		}
	}
}

func (s *Server) listOutput(arg string) (string, uint32) {
	rows := s.Rows()
	switch arg {
	case "", "list":
		return list.Text(rows, time.Now().Unix()), 0
	case "json", "list json":
		return list.JSON(rows), 0
	case "known_hosts", "list known_hosts":
		return list.KnownHosts(rows), 0
	}
	return list.Usage, 2
}
