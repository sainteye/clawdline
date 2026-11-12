package providerquota

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/limits"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

type Window struct {
	Name        string  `json:"name"`
	UsedPercent float64 `json:"used_percent"`
	ResetsAt    *int64  `json:"resets_at,omitempty"`
}
type Row struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	Windows    []Window `json:"windows"`
	ObservedAt *int64   `json:"observed_at,omitempty"`
	Stale      bool     `json:"stale"`
	Source     string   `json:"source"`
	Problem    string   `json:"problem,omitempty"`
	// CreditsInUse and CreditBalance are limits.Quota's: a spent Codex window
	// whose turns are charged to the account's credits.
	CreditsInUse  bool     `json:"credits_in_use,omitempty"`
	CreditBalance *float64 `json:"credit_balance,omitempty"`
}
type Snapshot struct {
	At        int64 `json:"at"`
	Providers []Row `json:"providers"`
	Updating  bool  `json:"updating"`
}
type slot struct {
	row        Row
	identity   string
	next       time.Time
	busy       bool
	generation uint64
}

// Service keeps one snapshot per provider. Reads never start agents or send prompts.
// Provider requests are single-flight per account and cannot hold up a console read.
// An expired Grok access token with a saved refresh token is renewed by the Grok CLI's
// own refresh before the account is called reauth_required; Clawdline never exchanges
// or writes the refresh token itself.
type Service struct {
	home     string
	local    *limits.Reader
	client   *http.Client
	mu       sync.Mutex
	slots    [CacheLimit]slot
	readFile func(string) ([]byte, error)
	getenv   func(string) string
	gemini   func(context.Context) ([]Window, string)
	renew    func(context.Context) error
}

func New(home string, local *limits.Reader) *Service {
	return &Service{home: home, local: local,
		client:   &http.Client{Timeout: RequestSecondsLimit * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		readFile: readSmall, getenv: os.Getenv}
}

// WithGemini selects the daemon's authenticated native quota reader before the
// service is published. It replaces the retired personal Gemini CLI API path.
func (s *Service) WithGemini(read func(context.Context) ([]Window, string)) *Service {
	s.gemini = read
	return s
}

// ErrRenewalUnavailable means the CLI could not be asked to renew this time.
var ErrRenewalUnavailable = errors.New("grok renewal unavailable")

// WithGrokRenewal selects how an expired Grok access token is renewed: a bounded
// run of the official CLI, which takes its own lock and rewrites auth.json.
func (s *Service) WithGrokRenewal(renew func(context.Context) error) *Service {
	s.renew = renew
	return s
}
func readSmall(path string) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, ResponseBytesLimit+1))
	if e == nil && len(b) > ResponseBytesLimit {
		return nil, errors.New("oversized credentials")
	}
	return b, e
}
func baseRow(id, label, source string) Row {
	return Row{ID: id, Label: label, Source: source, Windows: []Window{}}
}
func (s *Service) Snapshot(now time.Time) Snapshot { return s.snapshot(now, true) }

// Inspect reads existing evidence without credential access or provider requests.
func (s *Service) Inspect(now time.Time) Snapshot { return s.snapshot(now, false) }

func (s *Service) snapshot(now time.Time, refresh bool) Snapshot {
	out := Snapshot{At: now.Unix(), Providers: []Row{}}
	if s.local != nil {
		for _, p := range []struct{ id, label, source string }{{"claude", "Claude Code", "claude_statusline"}, {"codex", "Codex", "codex_rollout"}} {
			// A refreshing read may let the Claude reading ask Claude Code;
			// Doctor's inspection observes without asking.
			var q limits.Quota
			if refresh {
				q = s.local.Quota(p.id, now)
			} else {
				q = s.local.Observe(p.id, now)
			}
			source := p.source
			if q.FromClaudeCode {
				source = "provider_cli"
			}
			row := baseRow(p.id, p.label, source)
			row.Stale = q.Stale
			row.ObservedAt = q.ObservedAt
			for _, w := range q.Windows {
				if w.UsedPercent != nil {
					row.Windows = append(row.Windows, Window{Name: w.Name, UsedPercent: *w.UsedPercent, ResetsAt: w.ResetsAt})
				}
			}
			if q.Reason != "" {
				row.Problem = string(q.Reason)
			}
			row.CreditsInUse, row.CreditBalance = q.CreditsInUse, q.CreditBalance
			out.Providers = append(out.Providers, row)
		}
	}
	for i, p := range []struct{ id, label string }{{"gemini", "Gemini"}, {"grok", "Grok"}} {
		row, busy := s.provider(now, i, p.id, p.label, refresh)
		out.Providers = append(out.Providers, row)
		out.Updating = out.Updating || busy
	}
	return out
}

// Grok shares the global bar's account cache, cooldown and identity invalidation.
// A Session read must not refresh another provider or launch an agent.
func (s *Service) Grok(now time.Time) Row {
	row, _ := s.provider(now, 1, "grok", "Grok", true)
	return row
}

func (s *Service) provider(now time.Time, i int, id, label string, refresh bool) (Row, bool) {
	var cred credential
	var problem string
	source := "provider_api"
	if id == "gemini" && s.gemini != nil {
		cred.identity = "native"
		source = "provider_cli"
	} else if refresh {
		cred, problem = s.credentials(id, now)
	}
	s.mu.Lock()
	v := &s.slots[i]
	if v.row.ID == "" {
		v.row = baseRow(id, label, source)
	}
	if refresh && cred.identity != v.identity && !(cred.identity == "" && problem == "unreadable") {
		v.generation++
		v.row = baseRow(id, label, source)
		v.identity = cred.identity
		v.next = time.Time{}
	}
	renewFirst := problem == "reauth_required" && cred.renewable && s.canRenew(id)
	if problem != "" && !renewFirst {
		v.row.Problem = problem
		v.row.Stale = len(v.row.Windows) > 0
	} else if refresh && !v.busy && !now.Before(v.next) {
		v.busy = true
		v.next = now.Add(RefreshSecondsLimit * time.Second)
		generation := v.generation
		go s.refresh(i, id, cred, generation, renewFirst)
	}
	row := v.row
	row.Windows = append([]Window{}, row.Windows...)
	if row.ObservedAt != nil && now.Unix()-*row.ObservedAt >= RefreshSecondsLimit {
		row.Stale = true
	}
	busy := v.busy
	s.mu.Unlock()
	return row, busy
}

func (s *Service) canRenew(id string) bool { return id == "grok" && s.renew != nil }

// renewed runs the CLI's own refresh once and rereads the credential it wrote.
// A token that is still expired afterwards is the only reauth_required outcome.
func (s *Service) renewed(id string) (credential, string) {
	ctx, cancel := context.WithTimeout(context.Background(), RenewSecondsLimit*time.Second)
	defer cancel()
	err := s.renew(ctx) // The file, not the exit status, says whether renewal happened.
	c, problem := s.credentials(id, time.Now())
	if problem == "reauth_required" && errors.Is(err, ErrRenewalUnavailable) {
		// Grok could not be asked this time; that is not proof the sign-in has lapsed.
		problem = "unavailable"
	}
	return c, problem
}

func (s *Service) refresh(index int, id string, c credential, generation uint64, renewFirst bool) {
	original := c.identity
	var windows []Window
	var problem string
	var retry time.Time
	renewedOnce := false
	if renewFirst {
		c, problem = s.renewed(id)
		renewedOnce = true
	}
	if problem == "" {
		ctx, cancel := context.WithTimeout(context.Background(), RequestSecondsLimit*time.Second)
		windows, problem, retry = s.fetch(ctx, id, c)
		cancel()
		// A token the server rejected before its stated expiry gets one renewal and one retry.
		if problem == "reauth_required" && !renewedOnce && c.renewable && s.canRenew(id) {
			if c, problem = s.renewed(id); problem == "" {
				ctx, cancel := context.WithTimeout(context.Background(), RequestSecondsLimit*time.Second)
				windows, problem, retry = s.fetch(ctx, id, c)
				cancel()
			}
		}
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	v := &s.slots[index]
	if v.generation != generation || v.identity != original {
		v.busy = false
		return
	}
	v.busy = false
	// The renewed credential belongs to the same account; it must not reset the reading.
	if c.identity != "" {
		v.identity = c.identity
	}
	if retry.After(v.next) {
		v.next = retry
	}
	if problem != "" {
		v.row.Problem = problem
		v.row.Stale = len(v.row.Windows) > 0
		return
	}
	if len(windows) == 0 {
		v.row.Problem = "no_reading"
		v.row.Stale = len(v.row.Windows) > 0
		return
	}
	v.row.Windows = windows
	at := now.Unix()
	v.row.ObservedAt = &at
	v.row.Problem = ""
	v.row.Stale = false
}
func (s *Service) Reading() capacity.Reading {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for _, v := range s.slots {
		if v.row.ID != "" {
			n++
		}
	}
	return capacity.Reading{Known: true, Used: n, WindowSeconds: RefreshSecondsLimit}
}

type credential struct {
	token, user, project, identity string
	// renewable means the selected Grok entry carries a refresh token the CLI can use.
	renewable bool
}

func (s *Service) credentials(id string, now time.Time) (credential, string) {
	dir := filepath.Join(s.home, "."+id)
	if id == "grok" {
		if custom := s.getenv("GROK_HOME"); custom != "" {
			dir = custom
		}
	}
	name := "auth.json"
	if id == "gemini" {
		name = "oauth_creds.json"
	}
	b, e := s.readFile(filepath.Join(dir, name))
	if errors.Is(e, os.ErrNotExist) {
		return credential{}, "not_signed_in"
	}
	if e != nil {
		return credential{}, "unreadable"
	}
	hash := sha256.Sum256(b)
	c := credential{identity: hex.EncodeToString(hash[:])}
	if id == "gemini" {
		var a struct {
			AccessToken string  `json:"access_token"`
			Expiry      float64 `json:"expiry_date"`
		}
		if json.Unmarshal(b, &a) != nil {
			return c, "invalid_response"
		}
		c.token = a.AccessToken
		if a.Expiry > 0 && a.Expiry <= float64(now.UnixMilli()) {
			return c, "reauth_required"
		}
	} else {
		var entries map[string]struct {
			Key     string `json:"key"`
			User    string `json:"user_id"`
			Expires string `json:"expires_at"`
			Refresh string `json:"refresh_token"`
		}
		if json.Unmarshal(b, &entries) != nil {
			return c, "invalid_response"
		}
		// Only the CLI's own xAI issuer can receive its credential; alternate issuers are not guessed.
		keys := make([]string, 0, len(entries))
		for k := range entries {
			if k == "https://auth.x.ai" || strings.HasPrefix(k, "https://auth.x.ai::") {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		// Prefer the ordinary issuer, then a fresh issuer-qualified entry.
		if _, ok := entries["https://auth.x.ai"]; ok {
			keys = append([]string{"https://auth.x.ai"}, keys...)
		}
		for _, key := range keys {
			a := entries[key]
			if a.Key == "" {
				continue
			}
			c.token = a.Key
			c.user = a.User
			c.renewable = a.Refresh != ""
			if a.Expires != "" {
				expiry, e := time.Parse(time.RFC3339Nano, a.Expires)
				if e != nil {
					return c, "invalid_response"
				}
				if !expiry.After(now) {
					continue
				}
			}
			break
		}
		if c.token != "" {
			valid := false
			for _, key := range keys {
				a := entries[key]
				if a.Key != c.token {
					continue
				}
				expiry, e := time.Parse(time.RFC3339Nano, a.Expires)
				if a.Expires == "" || (e == nil && expiry.After(now)) {
					valid = true
				}
			}
			if !valid {
				return c, "reauth_required"
			}
		}
	}
	if strings.TrimSpace(c.token) == "" {
		return c, "not_signed_in"
	}
	return c, ""
}

// request returns only typed failure codes. Provider bodies, credentials and account identifiers
// never reach the browser, logs or error messages. There are no automatic HTTP retries.
func (s *Service) request(ctx context.Context, method, url string, c credential, body string) ([]byte, string, time.Time) {
	req, e := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	if e != nil {
		return nil, "unavailable", time.Time{}
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.HasPrefix(url, "https://cli-chat-proxy.grok.com/") {
		req.Header.Set("X-XAI-Token-Auth", "xai-grok-cli")
		if c.user != "" {
			req.Header.Set("x-userid", c.user)
		}
	}
	res, e := s.client.Do(req)
	if e != nil {
		return nil, "unavailable", time.Time{}
	}
	defer res.Body.Close()
	if res.StatusCode == 401 || res.StatusCode == 403 {
		return nil, "reauth_required", time.Time{}
	}
	if res.StatusCode == 429 {
		retry := time.Now().Add(RefreshSecondsLimit * time.Second)
		if n, e := strconv.ParseInt(res.Header.Get("Retry-After"), 10, 32); e == nil && n > 0 {
			retry = time.Now().Add(time.Duration(n) * time.Second)
		} else if t, e := http.ParseTime(res.Header.Get("Retry-After")); e == nil {
			retry = t
		}
		return nil, "rate_limited", retry
	}
	if res.StatusCode != http.StatusOK {
		return nil, "unavailable", time.Time{}
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, ResponseBytesLimit+1))
	if e != nil {
		return nil, "unavailable", time.Time{}
	}
	if len(b) > ResponseBytesLimit {
		return nil, "invalid_response", time.Time{}
	}
	return b, "", time.Time{}
}
func (s *Service) fetch(ctx context.Context, id string, c credential) ([]Window, string, time.Time) {
	if id == "gemini" {
		if s.gemini != nil {
			windows, problem := s.gemini(ctx)
			return windows, problem, time.Time{}
		}
		b, p, r := s.request(ctx, "POST", "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist", c, `{"metadata":{"ideType":"GEMINI_CLI","pluginType":"GEMINI"}}`)
		if p != "" {
			return nil, p, r
		}
		var project struct {
			Project json.RawMessage `json:"cloudaicompanionProject"`
		}
		if json.Unmarshal(b, &project) != nil {
			return nil, "invalid_response", time.Time{}
		}
		if json.Unmarshal(project.Project, &c.project) != nil {
			var obj struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(project.Project, &obj) == nil {
				c.project = obj.ID
			}
		}
		if c.project == "" {
			return nil, "no_reading", time.Time{}
		}
		payload, _ := json.Marshal(map[string]string{"project": c.project})
		b, p, r = s.request(ctx, "POST", "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota", c, string(payload))
		if p != "" {
			return nil, p, r
		}
		w, e := parseGemini(b)
		if e != nil {
			return nil, "invalid_response", time.Time{}
		}
		return w, "", time.Time{}
	}
	b, p, r := s.request(ctx, "GET", "https://cli-chat-proxy.grok.com/v1/billing?format=credits", c, "")
	if p != "" {
		return nil, p, r
	}
	w, e := parseGrok(b)
	if e != nil {
		return nil, "invalid_response", time.Time{}
	}
	if len(w) == 0 {
		b, p, r = s.request(ctx, "GET", "https://cli-chat-proxy.grok.com/v1/billing", c, "")
		if p != "" {
			return nil, p, r
		}
		w, e = parseGrok(b)
		if e != nil {
			return nil, "invalid_response", time.Time{}
		}
	}
	return w, "", time.Time{}
}
