package http

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/tunnel"
	"github.com/sainteye/clawdline-go/internal/contract"
)

// The tunnel's wiring (internal/adapters/tunnel holds its rules).
//
// It is applied at the three moments the Swift app applied it
// (`Sources/main.swift`): when the daemon starts, when the settings change, and
// when the devices do — pairing, a password, a revocation — because the
// refusal to open a tunnel while nothing is paired is waiting on exactly that
// answer, and a revocation that leaves nothing paired has to take a running
// tunnel down with it. Every one of those writes goes through this daemon's own
// routes (`/v1/settings`, `/v1/auth/*`), so those routes are where it is asked.
//
// Each apply reads its inputs afresh: the settings file as it is now, and
// whether anybody is let in from the gate's own authority — the one that
// judges every request the tunnel will carry.

// tunnels holds one supervisor per state directory, as `gates` holds one gate:
// the supervisor owns the one cloudflared that directory's config names.
var tunnels sync.Map // dir -> *tunnel.Supervisor

func (s *Server) tunnel() *tunnel.Supervisor {
	if t, ok := tunnels.Load(s.cfg.Dir); ok {
		return t.(*tunnel.Supervisor)
	}
	t, _ := tunnels.LoadOrStore(s.cfg.Dir, tunnel.New(tunnel.Options{Dir: s.cfg.Dir, Log: log.Printf}))
	return t.(*tunnel.Supervisor)
}

// StartTunnel stops a cloudflared an earlier run of this daemon left behind,
// then applies the settings. The daemon calls it once, before it listens.
func (s *Server) StartTunnel() {
	s.tunnel().Reclaim()
	s.applyTunnel()
}

// StopTunnel takes the tunnel down and waits for cloudflared to go. The daemon
// calls it on its way out: a tunnel that outlives its daemon is a public
// address nothing on screen admits to.
func (s *Server) StopTunnel() {
	if t, ok := tunnels.Load(s.cfg.Dir); ok {
		t.(*tunnel.Supervisor).Stop()
	}
}

// applyTunnel matches the tunnel to the settings and the devices as they are
// now.
//
// A settings file that cannot be read changes nothing: that is not an answer
// to "should the tunnel be open", in either direction, and the reason is
// logged.
func (s *Server) applyTunnel() {
	in, ok := s.tunnelInputs()
	if !ok {
		return
	}
	s.tunnel().Apply(in)
}

// tunnelInputs reads what a tunnel decision rests on, and refreshes the gate's
// hostname from the same reading.
func (s *Server) tunnelInputs() (tunnel.Inputs, bool) {
	v, err := s.settingsFile().Read()
	if err != nil {
		log.Printf("tunnel: the settings could not be read, so the tunnel was left as it was: %v", err)
		return tunnel.Inputs{}, false
	}
	g := s.gate()
	in := tunnel.Inputs{Port: s.cfg.Port, Paired: g.auth != nil && g.auth.IsConfigured()}
	in.Mode, _ = v.String("remote_tunnel")
	in.Name, _ = v.String("remote_tunnel_name")
	in.Hostname, _ = v.String("remote_hostname")
	in.Binary, _ = v.String("cloudflared_path")
	in.Remote, _ = v.Bool("remote")
	g.setHostname(in.Hostname)
	return in, true
}

// tunnelRoute is GET /v1/tunnel: what the tunnel is doing. This machine's own
// token only, because a quick tunnel's address is the access itself.
func (s *Server) tunnelRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request",
			"The tunnel is read with GET, and turned on and off in the settings.")
		return
	}
	if !requireLocal(w, r) {
		return
	}
	t := s.tunnel()
	st := t.Status()
	configured := ""
	if v, err := s.settingsFile().Read(); err == nil {
		configured, _ = v.String("cloudflared_path")
	}
	bin, installed := t.Installed(configured)
	writeJSON(w, contract.TunnelStatus{
		At:        time.Now().Unix(),
		Attempts:  int64(st.Attempts),
		Binary:    bin,
		Changed:   st.Changed.Unix(),
		Command:   st.Command,
		Config:    t.ConfigPath(),
		Installed: installed,
		Mode:      contract.TunnelMode(st.Mode),
		Reason:    st.Reason,
		State:     contract.TunnelState(st.Phase),
		URL:       st.URL,
	})
}
