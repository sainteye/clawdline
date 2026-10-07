package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// `clawdline webhook fire`: the caller's side of a schedule webhook. It runs
// on whatever machine wants a schedule started — a CI runner, another server —
// so it needs no daemon: one POST to the webhook URL, then GETs of that
// delivery's status until it ends, all with net/http.
//
// **The URL is a credential.** Its last path segment is the webhook's token,
// and anybody holding it can start the schedule. So it is never taken as an
// argument (a shell history and `ps` would keep it), only from a file or the
// environment, and nothing this command prints contains it: not its own
// messages, not the server's, and not a net/http error, whose text quotes the
// whole URL (`Post "https://…/<token>": dial tcp …`).
//
// The exit status is the answer a script branches on:
//
//	0   the schedule ran and succeeded
//	1   it ran and did not succeed, or an answer could not be read
//	2   it never reached the machine: expired, canceled, unknown, or the
//	    trigger could not be sent before the deliver-within deadline
//	3   refused: the machine refused the dispatch, the webhook is gone
//	    (rotated, disabled, moved), or Cloud refused the trigger (429, 4xx)
//	4   gave up waiting (--timeout) before it ended
//	64  the command was used wrongly: no URL, a bad flag, an argument
//
// Usage errors are 64 (EX_USAGE), not the 2 the other commands use, because
// 2 already means "never reached the machine" here.

const (
	webhookExitSuccess  = 0
	webhookExitFailed   = 1
	webhookExitNotThere = 2
	webhookExitRefused  = 3
	webhookExitGaveUp   = 4
	webhookExitUsage    = 64
)

const (
	// webhookURLEnv is the environment variable a URL may come from.
	webhookURLEnv = "CLAWDLINE_WEBHOOK_URL"
	// webhookAnswerLimit bounds what is read of any answer.
	webhookAnswerLimit = 64 << 10
	// webhookURLFileLimit bounds what is read of --url-file.
	webhookURLFileLimit = 8 << 10
	// The contract's range for deliver_within_seconds.
	webhookMinWithin = 10
	webhookMaxWithin = 86400
)

// webhookClock is the command's time: real in use, a fake one in the tests,
// so that a three-second poll does not cost a test three seconds.
type webhookClock struct {
	now   func() time.Time
	sleep func(time.Duration)
	// The status poll starts at pollFirst and grows by half up to pollMax.
	pollFirst, pollMax time.Duration
	// A trigger that failed on the way is sent again after retryFirst,
	// doubling up to retryMax.
	retryFirst, retryMax time.Duration
	// client sends every request.
	client *http.Client
}

func realWebhookClock() webhookClock {
	return webhookClock{
		now:        time.Now,
		sleep:      time.Sleep,
		pollFirst:  3 * time.Second,
		pollMax:    10 * time.Second,
		retryFirst: time.Second,
		retryMax:   30 * time.Second,
		client:     webhookClient(),
	}
}

// webhookClient does not follow redirects: a redirected POST becomes a GET,
// and a redirect would carry the token to wherever it points.
func webhookClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func webhookCommand(args []string) {
	if len(args) < 1 || args[0] != "fire" {
		webhookFireUsage(os.Stderr)
		os.Exit(webhookExitUsage)
	}
	os.Exit(runWebhookFire(os.Stdout, os.Stderr, args[1:], os.Getenv, realWebhookClock()))
}

func webhookFireUsage(w io.Writer) {
	fmt.Fprintln(w, cliCopy("misc", "webhook_fire.usage_clawdline_webhook_fire_url_fi.fb14730c", "usage: clawdline webhook fire [--url-file path] [--deliver-within 60s] [--timeout 60m] [--no-wait]"))
	fmt.Fprintf(w, cliCopy("misc", "webhook_fire.usage_source", "  start a schedule through its webhook; no daemon needed. The URL is read from --url-file or %s, never from an argument\n"), webhookURLEnv)
	fmt.Fprintln(w, "  stdout, one line: success|failed|not_delivered|refused|gave_up delivery=<id|-> [state=<s>] [outcome=<o>] [refusal_code=<c>] [code=<c> status=<n>] [reason=unreachable]")
	fmt.Fprintln(w, cliCopy("misc", "webhook_fire.no_wait_prints_only_the_delivery_id.8324d81b", "  --no-wait prints only the delivery id. Exit: 0 success, 1 failed, 2 never reached the machine, 3 refused, 4 gave up waiting, 64 usage"))
}

// runWebhookFire is the command, answering its exit status.
func runWebhookFire(stdout, stderr io.Writer, args []string, getenv func(string) string, clock webhookClock) int {
	fs := flag.NewFlagSet("webhook fire", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	urlFile := fs.String("url-file", "", "a file holding the webhook URL")
	within := fs.String("deliver-within", "60s", "how long the delivery may wait to reach the machine")
	timeout := fs.String("timeout", "60m", "how long to wait for the outcome once accepted")
	noWait := fs.Bool("no-wait", false, "print the delivery id once accepted and stop")
	// A parse error's text can quote the argument it did not understand, and
	// that argument may be the URL; it is never printed.
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, cliCopy("misc", "webhook_fire.clawdline_webhook_fire_a_flag_could.74b17d20", "clawdline webhook fire: a flag could not be read"))
		webhookFireUsage(stderr)
		return webhookExitUsage
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, cliCopy("misc", "webhook_fire.credential_argument", "clawdline webhook fire: takes no arguments. The webhook URL is a credential: "+
			"put it in a file for --url-file, or in %s\n"), webhookURLEnv)
		webhookFireUsage(stderr)
		return webhookExitUsage
	}
	withinSeconds, ok := webhookWithinSeconds(*within)
	if !ok {
		fmt.Fprintf(stderr, cliCopy("misc", "webhook_fire.clawdline_webhook_fire_deliver_with.f74a0534", "clawdline webhook fire: --deliver-within must be a duration from %ds to %ds, in whole seconds\n"),
			webhookMinWithin, webhookMaxWithin)
		return webhookExitUsage
	}
	wait, err := time.ParseDuration(*timeout)
	if err != nil || wait <= 0 {
		fmt.Fprintln(stderr, cliCopy("misc", "webhook_fire.clawdline_webhook_fire_timeout_must.515b0e25", "clawdline webhook fire: --timeout must be a positive duration, like 60m"))
		return webhookExitUsage
	}
	raw, problem := webhookURL(*urlFile, getenv)
	if problem != "" {
		fmt.Fprintln(stderr, cliCopy("misc", "webhook_fire.clawdline_webhook_fire.76515ede", "clawdline webhook fire:"), problem)
		return webhookExitUsage
	}
	f := &webhookFire{
		stdout: stdout,
		stderr: stderr,
		clock:  clock,
		url:    raw,
		secret: webhookSecrets(raw),
	}
	return f.run(withinSeconds, wait, *noWait)
}

// webhookWithinSeconds is --deliver-within as the contract's whole seconds.
func webhookWithinSeconds(s string) (int, bool) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, false
	}
	n := int64(d.Round(time.Second) / time.Second)
	if n < webhookMinWithin || n > webhookMaxWithin {
		return 0, false
	}
	return int(n), true
}

// webhookURL is the URL from --url-file, else from the environment, or why
// there is none. No answer here quotes the URL.
func webhookURL(file string, getenv func(string) string) (string, string) {
	var raw string
	if file != "" {
		fh, err := os.Open(file)
		if err != nil {
			return "", cliCopy("misc", "webhook_fire.file_unreadable", "--url-file could not be read: ") + err.Error()
		}
		data, err := io.ReadAll(io.LimitReader(fh, webhookURLFileLimit+1))
		_ = fh.Close()
		if err != nil {
			return "", cliCopy("misc", "webhook_fire.file_unreadable", "--url-file could not be read: ") + err.Error()
		}
		if len(data) > webhookURLFileLimit {
			return "", fmt.Sprintf(cliCopy("misc", "webhook_fire.file_too_large", "--url-file is larger than %d bytes; it should hold one URL"), webhookURLFileLimit)
		}
		raw = strings.TrimSpace(string(data))
		if raw == "" {
			return "", cliCopy("misc", "webhook_fire.file_empty", "--url-file is empty")
		}
	} else {
		raw = strings.TrimSpace(getenv(webhookURLEnv))
		if raw == "" {
			return "", fmt.Sprintf(cliCopy("misc", "webhook_fire.url_missing", "no webhook URL: pass --url-file <path>, or set %s"), webhookURLEnv)
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", cliCopy("misc", "webhook_fire.url_unreadable", "the webhook URL could not be read as a URL")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", cliCopy("misc", "webhook_fire.url_parts_forbidden", "the webhook URL has a query, a fragment or a user name; the webhook's URL has none")
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && webhookLoopback(u.Hostname()):
		// Plain http only to this machine itself, where nothing on a network
		// can read the token: a local test server.
	default:
		return "", cliCopy("misc", "webhook_fire.url_scheme", "the webhook URL must be https (plain http only to this machine itself)")
	}
	return strings.TrimRight(raw, "/"), ""
}

func webhookLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// webhookSecrets is every string that must not be printed: the URL as given,
// and each path segment long enough to be the token.
func webhookSecrets(raw string) []string {
	secrets := []string{raw}
	if u, err := url.Parse(raw); err == nil {
		for _, seg := range strings.Split(u.EscapedPath(), "/") {
			if len(seg) >= 16 {
				secrets = append(secrets, seg)
				if un, err := url.PathUnescape(seg); err == nil && un != seg {
					secrets = append(secrets, un)
				}
			}
		}
	}
	return secrets
}

// webhookFire is one run of the command.
type webhookFire struct {
	stdout, stderr io.Writer
	clock          webhookClock
	url            string
	secret         []string
}

// say writes one line on stderr with anything secret taken out of it.
func (f *webhookFire) say(format string, args ...any) {
	fmt.Fprintln(f.stderr, f.redact(fmt.Sprintf(cliCopy("misc", "webhook_fire.clawdline_webhook_fire.f5bb94fc", "clawdline webhook fire: ")+format, args...)))
}

// result writes the one stdout line.
func (f *webhookFire) result(line string) {
	fmt.Fprintln(f.stdout, f.redact(line))
}

func (f *webhookFire) redact(s string) string {
	for _, secret := range f.secret {
		s = strings.ReplaceAll(s, secret, "<webhook>")
	}
	return s
}

// webhookAccepted is the 202. Keys this command does not know are ignored.
type webhookAccepted struct {
	Schema     string `json:"schema"`
	DeliveryID string `json:"delivery_id"`
	DeliverBy  string `json:"deliver_by"`
	State      string `json:"state"`
}

// webhookStatus is a status answer.
type webhookStatus struct {
	Schema      string  `json:"schema"`
	DeliveryID  string  `json:"delivery_id"`
	State       string  `json:"state"`
	Outcome     *string `json:"outcome"`
	RefusalCode *string `json:"refusal_code"`
	DeliverBy   string  `json:"deliver_by"`
	UpdatedAt   string  `json:"updated_at"`
}

func (f *webhookFire) run(withinSeconds int, wait time.Duration, noWait bool) int {
	accepted, code := f.trigger(withinSeconds)
	if code >= 0 {
		return code
	}
	id := accepted.DeliveryID
	if noWait {
		fmt.Fprintln(f.stdout, id)
		return webhookExitSuccess
	}
	if accepted.DeliverBy != "" {
		f.say(cliCopy("misc", "webhook_fire.say_delivery_s_accepted_to_reach_the_m.10ab38a1", "delivery %s: accepted, to reach the machine by %s"), id, webhookWord(accepted.DeliverBy))
	} else {
		f.say(cliCopy("misc", "webhook_fire.say_delivery_s_accepted.3a00eda0", "delivery %s: accepted"), id)
	}
	return f.poll(accepted, wait)
}

// trigger sends the POST, again with the same Idempotency-Key after a failure
// on the way or a 5xx, for as long as the delivery could still be on time. It
// answers the 202, or the exit status when there is none (-1 when there is a
// 202).
func (f *webhookFire) trigger(withinSeconds int) (webhookAccepted, int) {
	key := webhookKey()
	body := []byte(fmt.Sprintf(`{"deliver_within_seconds":%d}`, withinSeconds))
	deadline := f.clock.now().Add(time.Duration(withinSeconds) * time.Second)
	backoff := f.clock.retryFirst
	for attempt := 1; ; attempt++ {
		req, err := http.NewRequest(http.MethodPost, f.url, bytes.NewReader(body))
		if err != nil {
			f.say(cliCopy("misc", "webhook_fire.say_the_request_could_not_be_made.5748132c", "the request could not be made"))
			return webhookAccepted{}, webhookExitUsage
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		status, answer, why := f.send(req)
		switch {
		case why == "" && status >= 200 && status < 300:
			var a webhookAccepted
			if json.Unmarshal(answer, &a) != nil || !webhookToken(a.DeliveryID) {
				f.say(cliCopy("misc", "webhook_fire.say_the_trigger_was_accepted_d_but_its.0f65c4c9", "the trigger was accepted (%d) but its answer names no delivery id; its outcome cannot be followed"), status)
				f.result("failed delivery=- reason=unreadable_answer")
				return webhookAccepted{}, webhookExitFailed
			}
			return a, -1
		case why == "" && status < 500:
			code := webhookErrorCode(answer)
			f.say(cliCopy("misc", "webhook_fire.say_the_trigger_was_refused_d_s.f9b3059d", "the trigger was refused: %d %s"), status, code)
			f.result(fmt.Sprintf("refused delivery=- code=%s status=%d", code, status))
			return webhookAccepted{}, webhookExitRefused
		}
		if why == "" {
			why = fmt.Sprintf("%d %s", status, webhookErrorCode(answer))
		}
		if !f.clock.now().Add(backoff).Before(deadline) {
			f.say(cliCopy("misc", "webhook_fire.say_attempt_d_failed_s_the_deliver_wit.09414794", "attempt %d failed (%s); the deliver-within deadline leaves no time for another"), attempt, why)
			f.result("not_delivered delivery=- reason=unreachable")
			return webhookAccepted{}, webhookExitNotThere
		}
		f.say(cliCopy("misc", "webhook_fire.say_attempt_d_failed_s_sending_it_agai.0152fed6", "attempt %d failed (%s); sending it again in %s with the same Idempotency-Key"), attempt, why, backoff)
		f.clock.sleep(backoff)
		backoff = min(2*backoff, f.clock.retryMax)
	}
}

// poll reads the delivery's status until it ends or wait runs out.
func (f *webhookFire) poll(accepted webhookAccepted, wait time.Duration) int {
	id := accepted.DeliveryID
	statusURL := f.url + "/deliveries/" + url.PathEscape(id)
	last := "queued"
	if webhookToken(accepted.State) {
		last = accepted.State
	}
	deadline := f.clock.now().Add(wait)
	interval := f.clock.pollFirst
	for {
		remaining := deadline.Sub(f.clock.now())
		if remaining <= 0 {
			f.say(cliCopy("misc", "webhook_fire.say_delivery_s_gave_up_waiting_after_s.7d8d2070", "delivery %s: gave up waiting after %s; it was last %s"), id, wait, last)
			f.result(fmt.Sprintf("gave_up delivery=%s state=%s", id, last))
			return webhookExitGaveUp
		}
		f.clock.sleep(min(interval, remaining))
		interval = min(interval*3/2, f.clock.pollMax)

		req, err := http.NewRequest(http.MethodGet, statusURL, nil)
		if err != nil {
			f.say(cliCopy("misc", "webhook_fire.say_the_request_could_not_be_made.5748132c", "the request could not be made"))
			return webhookExitFailed
		}
		status, answer, why := f.send(req)
		switch {
		case why != "":
			f.say(cliCopy("misc", "webhook_fire.say_delivery_s_status_could_not_be_rea.bc22e5ab", "delivery %s: status could not be read (%s); still waiting"), id, why)
			continue
		case status >= 500:
			f.say(cliCopy("misc", "webhook_fire.say_delivery_s_status_could_not_be_rea.24ba6caf", "delivery %s: status could not be read (%d); still waiting"), id, status)
			continue
		case status == http.StatusTooManyRequests:
			// The status read has its own fence (120 a minute), far above
			// this poll's rate; if it is reached anyway, slow down and keep
			// reading rather than give up on a delivery that may be running.
			interval = f.clock.pollMax
			f.say(cliCopy("misc", "webhook_fire.say_delivery_s_status_reads_are_being.acdec31e", "delivery %s: status reads are being limited; reading less often"), id)
			continue
		case status == http.StatusNotFound:
			code := webhookErrorCode(answer)
			if code == "not_found" {
				// Ambiguous, decided here: the webhook is still there but
				// has no record of this delivery. Cloud answered 202 for it
				// moments ago, so the one reading that fits is that it was
				// dropped before reaching the machine: exit 2, not 3, since
				// nothing refused it.
				f.say(cliCopy("misc", "webhook_fire.say_delivery_s_cloud_has_no_record_of.74017dee", "delivery %s: Cloud has no record of it"), id)
				f.result(fmt.Sprintf("not_delivered delivery=%s state=unknown code=not_found", id))
				return webhookExitNotThere
			}
			f.say(cliCopy("misc", "webhook_fire.say_delivery_s_status_refused_404_s.2dbb798c", "delivery %s: status refused: 404 %s"), id, code)
			f.result(fmt.Sprintf("refused delivery=%s code=%s status=404", id, code))
			return webhookExitRefused
		case status < 200 || status >= 300:
			code := webhookErrorCode(answer)
			f.say(cliCopy("misc", "webhook_fire.say_delivery_s_status_refused_d_s.8d52a848", "delivery %s: status refused: %d %s"), id, status, code)
			f.result(fmt.Sprintf("refused delivery=%s code=%s status=%d", id, code, status))
			return webhookExitRefused
		}
		var st webhookStatus
		if json.Unmarshal(answer, &st) != nil || !webhookToken(st.State) {
			f.say(cliCopy("misc", "webhook_fire.say_delivery_s_a_status_answer_could_n.324cfa1c", "delivery %s: a status answer could not be read; still waiting"), id)
			continue
		}
		if st.State != last {
			last = st.State
			f.say(cliCopy("misc", "webhook_fire.say_delivery_s_s.dcf82996", "delivery %s: %s"), id, last)
		}
		if code, done := f.final(id, st); done {
			return code
		}
	}
}

// final is the exit status for a state that ends the delivery.
func (f *webhookFire) final(id string, st webhookStatus) (int, bool) {
	switch st.State {
	case "terminal":
		outcome := "none"
		if st.Outcome != nil && webhookToken(*st.Outcome) {
			outcome = *st.Outcome
		}
		if outcome == "success" {
			f.result(fmt.Sprintf("success delivery=%s state=terminal outcome=success", id))
			return webhookExitSuccess, true
		}
		f.result(fmt.Sprintf("failed delivery=%s state=terminal outcome=%s", id, outcome))
		return webhookExitFailed, true
	case "expired", "canceled":
		f.result(fmt.Sprintf("not_delivered delivery=%s state=%s", id, st.State))
		return webhookExitNotThere, true
	case "dispatch_refused":
		code := "none"
		if st.RefusalCode != nil && webhookToken(*st.RefusalCode) {
			code = *st.RefusalCode
		}
		f.result(fmt.Sprintf("refused delivery=%s state=dispatch_refused refusal_code=%s", id, code))
		return webhookExitRefused, true
	}
	// Every other state, including one this build does not know yet, is on
	// its way.
	return 0, false
}

// send makes one request, answering its status and body, or in words why
// there is none. The words never quote the URL.
func (f *webhookFire) send(req *http.Request) (int, []byte, string) {
	resp, err := f.clock.client.Do(req)
	if err != nil {
		return 0, nil, webhookTransportError(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, webhookAnswerLimit))
	if err != nil {
		return 0, nil, "the answer was cut off"
	}
	return resp.StatusCode, body, ""
}

// webhookTransportError is a failed request in words without its URL or its
// address: a *url.Error quotes the whole URL, and a *net.OpError the host and
// port it dialled.
func webhookTransportError(err error) string {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "the host name could not be looked up"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return "timed out"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timed out"
	}
	var op *net.OpError
	if errors.As(err, &op) {
		inner := op.Err
		// A syscall error under an os.SyscallError: "connection refused".
		var sys *os.SyscallError
		if errors.As(inner, &sys) {
			inner = sys.Err
		}
		return op.Op + ": " + inner.Error()
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Op + ": " + ue.Err.Error()
	}
	return err.Error()
}

// webhookErrorCode is a refusal's code, from either envelope this product
// uses — `{"error":{"code":…}}` and `{"error":"<code>"}` — or a bare
// `{"code":…}`; "-" when the answer has none.
func webhookErrorCode(body []byte) string {
	var env struct {
		Error json.RawMessage `json:"error"`
		Code  string          `json:"code"`
	}
	if json.Unmarshal(body, &env) != nil {
		return "-"
	}
	var nested struct {
		Code string `json:"code"`
	}
	var flat string
	switch {
	case json.Unmarshal(env.Error, &nested) == nil && webhookToken(nested.Code):
		return nested.Code
	case json.Unmarshal(env.Error, &flat) == nil && webhookToken(flat):
		return flat
	case webhookToken(env.Code):
		return env.Code
	}
	return "-"
}

// webhookToken reports whether a value from the server can stand in the
// stdout line as one word: short, and letters, digits, and - _ . : + only.
func webhookToken(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == ':', r == '+':
		default:
			return false
		}
	}
	return true
}

// webhookWord is a server value safe to print, or "-".
func webhookWord(s string) string {
	if webhookToken(s) {
		return s
	}
	return "-"
}

// webhookKey is the run's one Idempotency-Key: 32 hex characters.
func webhookKey() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
