package http

// Local Cloud onboarding. A browser holding this daemon's own token can start
// device authorization, enable the line, and approve effectful Cloud requests.
// Account approval never grants operation rights by itself.

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

type cloudLoginAttempt struct {
	start   adaptercloud.LoginStart
	name    string
	apiBase string
	expires time.Time
	next    time.Time
	step    time.Duration
	phase   string
	code    string
}

type cloudLoginView struct {
	Phase           string `json:"phase"`
	UserCode        string `json:"user_code,omitempty"`
	VerificationURL string `json:"verification_url,omitempty"`
	ExpiresAt       int64  `json:"expires_at,omitempty"`
	NextPollAt      int64  `json:"next_poll_at,omitempty"`
	Code            string `json:"code,omitempty"`
}

func (a cloudLoginAttempt) view() cloudLoginView {
	v := cloudLoginView{Phase: a.phase, Code: a.code}
	if a.phase == "waiting" {
		v.UserCode = a.start.UserCode
		v.VerificationURL = a.start.VerificationURIComplete
		v.ExpiresAt = a.expires.Unix()
		v.NextPollAt = a.next.Unix()
	}
	if v.Phase == "" {
		v.Phase = "idle"
	}
	return v
}

// SetCloudReload installs the daemon's bounded line replacement operation.
// A test server without a running daemon may leave it unset.
func (s *Server) SetCloudReload(reload func() error) { s.cloudReload = reload }

func (s *Server) cloudLoginRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPost {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "Cloud sign-in is started with POST and read with GET.")
		return
	}
	s.cloudLoginMu.Lock()
	defer s.cloudLoginMu.Unlock()
	file := nextconfig.Open(s.cfg.Dir)
	settings, err := adaptercloud.ReadSettings(file)
	if err != nil {
		writeCloudOnboardingFailure(w, err)
		return
	}
	keys, err := cloudkeys.Open(s.cfg.Dir)
	if err != nil {
		writeCloudOnboardingFailure(w, err)
		return
	}
	identityStore := adaptercloud.NewIdentityStore(keys.Dir())
	_, found, err := identityStore.Load()
	if err != nil {
		writeCloudOnboardingFailure(w, err)
		return
	}
	now := time.Now()
	if s.cloudLogin.phase == "waiting" && !now.Before(s.cloudLogin.expires) {
		s.cloudLogin.phase, s.cloudLogin.code = "failed", "login_expired"
	}
	if r.Method == http.MethodPost {
		var body struct {
			Replace bool `json:"replace"`
		}
		if r.Body != nil {
			dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, settingsRequestBodyLimit))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
				writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "Send {\"replace\": true} to sign this machine in again.")
				return
			}
			var extra any
			if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
				writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "Send one JSON object only.")
				return
			}
		}
		if s.cloudLogin.phase == "waiting" {
			writeJSON(w, s.cloudLogin.view())
			return
		}
		if found && !body.Replace {
			writeJSON(w, cloudLoginView{Phase: "complete"})
			return
		}
		key, err := domaincloud.LoadOrCreateDeviceKey(keys, rand.Reader)
		if err != nil {
			writeCloudOnboardingFailure(w, err)
			return
		}
		name := cloudtransport.MachineName(cloudtransport.HostName(), runtime.GOOS, "", settings.MachineName)
		client := adaptercloud.NewAccountClient(settings.APIBase)
		start, err := client.StartLogin(r.Context(), name, runtime.GOOS, key.PublicKey(), s.version)
		if err != nil {
			writeCloudOnboardingFailure(w, err)
			return
		}
		if start.ExpiresIn <= 0 || start.VerificationURIComplete == "" {
			writeAuthRefusal(w, http.StatusBadGateway, "login_bad_response", "The Cloud sign-in page or expiry was missing. Try again.")
			return
		}
		interval := time.Duration(start.Interval) * time.Second
		if interval < 5*time.Second {
			interval = 5 * time.Second
		}
		s.cloudLogin = cloudLoginAttempt{start: start, name: name, apiBase: settings.APIBase,
			expires: now.Add(time.Duration(start.ExpiresIn) * time.Second), next: now.Add(interval), step: interval, phase: "waiting"}
		writeJSON(w, s.cloudLogin.view())
		return
	}
	if found && s.cloudLogin.phase != "waiting" && s.cloudLogin.phase != "failed" {
		writeJSON(w, cloudLoginView{Phase: "complete"})
		return
	}
	if s.cloudLogin.phase != "waiting" || now.Before(s.cloudLogin.next) {
		writeJSON(w, s.cloudLogin.view())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	client := adaptercloud.NewAccountClient(s.cloudLogin.apiBase)
	poll, err := client.PollLogin(ctx, s.cloudLogin.start.DeviceCode)
	if err != nil {
		// The code remains usable after a temporary network failure. The next
		// request may retry only after the control plane's interval.
		s.cloudLogin.next = now.Add(s.cloudLogin.step)
		writeCloudOnboardingFailure(w, err)
		return
	}
	switch poll.Status {
	case adaptercloud.LoginPending:
		s.cloudLogin.next = now.Add(s.cloudLogin.step)
	case adaptercloud.LoginSlowDown:
		if poll.RetryAfterSeconds > 0 {
			s.cloudLogin.step = time.Duration(poll.RetryAfterSeconds) * time.Second
		} else {
			s.cloudLogin.step += time.Second
		}
		s.cloudLogin.next = now.Add(s.cloudLogin.step)
	case adaptercloud.LoginDenied:
		s.cloudLogin.phase, s.cloudLogin.code = "failed", "login_denied"
	case adaptercloud.LoginExpired:
		s.cloudLogin.phase, s.cloudLogin.code = "failed", "login_expired"
	case adaptercloud.LoginComplete:
		if poll.AccountID == "" || poll.MachineID == "" || poll.MachineCredential == "" {
			writeAuthRefusal(w, http.StatusBadGateway, "login_bad_response", "The approval did not name a machine. Try again.")
			return
		}
		identity := adaptercloud.Identity{AccountID: poll.AccountID, MachineID: poll.MachineID,
			MachineCredential: poll.MachineCredential, APIBase: s.cloudLogin.apiBase, Name: s.cloudLogin.name}
		if err := identityStore.Save(identity); err != nil {
			writeCloudOnboardingFailure(w, err)
			return
		}
		s.cloudLogin = cloudLoginAttempt{phase: "complete"}
		if settings.Enabled && s.cloudReload != nil {
			if err := s.cloudReload(); err != nil {
				writeCloudOnboardingFailure(w, err)
				return
			}
		}
	default:
		writeAuthRefusal(w, http.StatusBadGateway, "login_bad_response", "The Cloud sign-in status was not recognized. Try again.")
		return
	}
	writeJSON(w, s.cloudLogin.view())
}

func (s *Server) cloudEnabledRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "The Cloud switch is changed with POST.")
		return
	}
	on, ok := readCloudSwitch(w, r)
	if !ok {
		return
	}
	file := nextconfig.Open(s.cfg.Dir)
	if on {
		keys, err := cloudkeys.Open(s.cfg.Dir)
		if err != nil {
			writeCloudOnboardingFailure(w, err)
			return
		}
		if _, found, err := adaptercloud.NewIdentityStore(keys.Dir()).Load(); err != nil {
			writeCloudOnboardingFailure(w, err)
			return
		} else if !found {
			writeAuthRefusal(w, http.StatusConflict, "cloud_not_signed_in", "Approve this machine's Cloud sign-in before connecting.")
			return
		}
		if s.cloudReload == nil {
			writeAuthRefusal(w, http.StatusServiceUnavailable, "cloud_runtime_unavailable", "The Cloud connection is not available in this daemon.")
			return
		}
	}
	settings, err := adaptercloud.ReadSettings(file)
	if err != nil {
		writeCloudOnboardingFailure(w, err)
		return
	}
	reloadNeeded := settings.Enabled != on
	if settings.Enabled != on {
		if err := adaptercloud.SetEnabled(file, on); err != nil {
			writeCloudOnboardingFailure(w, err)
			return
		}
	}
	if on && !reloadNeeded {
		line, present := cloudLines.Load(s.cfg.Dir)
		reloadNeeded = !present || !line.(CloudLine).Status().Enabled || line.(CloudLine).Status().State == "stopped"
	}
	if reloadNeeded && s.cloudReload != nil {
		if err := s.cloudReload(); err != nil {
			writeCloudOnboardingFailure(w, err)
			return
		}
	}
	writeJSON(w, map[string]bool{"enabled": on})
}

// cloudCommandsRoute acknowledges the older setup guide's enable request.
// The Cloud connection now owns machine-wide access.
func (s *Server) cloudCommandsRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "The legacy Cloud setup request requires POST.")
		return
	}
	on, ok := readCloudSwitch(w, r)
	if !ok {
		return
	}
	if !on {
		writeAuthRefusal(w, http.StatusGone, "bad_request", "Cloud operations follow this machine's Cloud connection and the device's grant.")
		return
	}
	settings, err := adaptercloud.ReadSettings(nextconfig.Open(s.cfg.Dir))
	if err != nil {
		writeCloudOnboardingFailure(w, err)
		return
	}
	writeJSON(w, map[string]bool{"commands": settings.Enabled})
}

// cloudDirectRoute changes the machine's shared Terminal and Session carrier.
// Replacing an enabled line retires its current direct peers immediately.
func (s *Server) cloudDirectRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "The direct connection switch is changed with POST.")
		return
	}
	on, ok := readCloudSwitch(w, r)
	if !ok {
		return
	}
	file := nextconfig.Open(s.cfg.Dir)
	settings, err := adaptercloud.ReadSettings(file)
	if err != nil {
		writeCloudOnboardingFailure(w, err)
		return
	}
	if settings.Enabled && s.cloudReload == nil {
		writeAuthRefusal(w, http.StatusServiceUnavailable, "cloud_runtime_unavailable", "The Cloud connection is not available in this daemon.")
		return
	}
	if settings.TerminalDirect != on {
		if err := adaptercloud.SetTerminalDirect(file, on); err != nil {
			writeCloudOnboardingFailure(w, err)
			return
		}
	}
	if settings.Enabled {
		if err := s.cloudReload(); err != nil {
			writeCloudOnboardingFailure(w, err)
			return
		}
	}
	writeJSON(w, map[string]bool{"terminal_direct": on})
}

// cloudDirectLocalNetworkRoute changes whether the direct carrier may use
// private IPv4 addresses on the same network. Like the direct switch it
// replaces an enabled line, which retires the current direct peers, so every
// later offer and answer is filtered by the new choice.
func (s *Server) cloudDirectLocalNetworkRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "The same-network connection switch is changed with POST.")
		return
	}
	on, ok := readCloudSwitch(w, r)
	if !ok {
		return
	}
	file := nextconfig.Open(s.cfg.Dir)
	settings, err := adaptercloud.ReadSettings(file)
	if err != nil {
		writeCloudOnboardingFailure(w, err)
		return
	}
	// A paired Cloud viewer may only enable this one setting. The bridge's
	// write gate has already rechecked its permission. Keep the relay alive
	// for the receipt; subsequent candidate exchanges read the new policy.
	if cloudtransport.ViaCloud(r.Context()) {
		if !on || cloudtransport.CloudViewerOf(r.Context()) == "" || !settings.Enabled || !settings.TerminalDirect {
			writeAuthRefusal(w, http.StatusForbidden, "forbidden", "This machine must have Cloud and direct connections enabled to enable same-network connections.")
			return
		}
		if err := adaptercloud.SetDirectLocalNetwork(file, true); err != nil {
			writeCloudOnboardingFailure(w, err)
			return
		}
		writeJSON(w, map[string]bool{"direct_local_network": true})
		return
	}
	if settings.Enabled && s.cloudReload == nil {
		writeAuthRefusal(w, http.StatusServiceUnavailable, "cloud_runtime_unavailable", "The Cloud connection is not available in this daemon.")
		return
	}
	if settings.DirectLocalNetwork != on {
		if err := adaptercloud.SetDirectLocalNetwork(file, on); err != nil {
			writeCloudOnboardingFailure(w, err)
			return
		}
	}
	if settings.Enabled {
		if err := s.cloudReload(); err != nil {
			writeCloudOnboardingFailure(w, err)
			return
		}
	}
	writeJSON(w, map[string]bool{"direct_local_network": on})
}

func readCloudSwitch(w http.ResponseWriter, r *http.Request) (bool, bool) {
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, settingsRequestBodyLimit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || body.Enabled == nil {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "Send {\"enabled\": true} or {\"enabled\": false}.")
		return false, false
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "Send one JSON object only.")
		return false, false
	}
	return *body.Enabled, true
}

func writeCloudOnboardingFailure(w http.ResponseWriter, err error) {
	failure := adaptercloud.DescribeFailure(err)
	if failure.Kind != "" {
		writeRawAuthRefusal(w, http.StatusBadGateway, failure.Code, failure.String())
		return
	}
	code := "cloud_local_failed"
	if errors.Is(err, os.ErrPermission) {
		code = "cloud_store_forbidden"
	}
	writeRawAuthRefusal(w, http.StatusInternalServerError, code, strings.TrimSpace(err.Error()))
}
