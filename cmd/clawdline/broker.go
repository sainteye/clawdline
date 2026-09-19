package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/devices"
	"github.com/sainteye/clawdline-go/internal/adapters/skillfile"
	"github.com/sainteye/clawdline-go/internal/config"
	"github.com/sainteye/clawdline-go/internal/contract"
)

// The thin commands a session runs instead of hand-typed curl: each one is a
// single request to this machine's daemon, and says what the daemon said.
//
// **The credential stays in this process.** It is read from this app's own
// state directory — CLAWDLINE_NEXT_DIR, else ~/.config/clawdline-next — and
// never from the Swift app's ~/.config/clawdline. It is not taken as an
// argument, so it is never in argv where `ps` shows it; it is never printed,
// and a daemon answer that somehow carried it would be printed with it
// masked. The old helper kept the same rule, and a session's transcript is
// where a token would otherwise end up.

// brokerAnswerLimit is how much of an answer is read. The largest answers
// these commands ask for — the landings and the assistants — are tens of
// kilobytes.
const brokerAnswerLimit = 4 << 20

// brokerTokenLimit is the most of the token file read.
const brokerTokenLimit = 4 << 10

// brokerTimeout is how long a command waits for its answer. It is long on
// purpose: a relay reads the whole session inventory before it types, which
// took more than 30 seconds on a machine whose iTerm answers slowly, and a
// command that hangs up while the daemon is typing leaves an effect that ran
// and could not be recorded (measured on 2026-09-19; see docs/skill.md).
const brokerTimeout = 2 * time.Minute

// broker is one daemon, reached with this machine's orchestrator token.
type broker struct {
	base   string
	token  string
	client *http.Client
}

// openBroker resolves the daemon and reads the token. port 0 means
// CLAWDLINE_NEXT_PORT, else the default.
func openBroker(port int) (*broker, error) {
	if port == 0 {
		p, err := daemonPort()
		if err != nil {
			return nil, err
		}
		port = p
	}
	token, err := machineToken(config.Dir())
	if err != nil {
		return nil, err
	}
	return &broker{
		base:   "http://127.0.0.1:" + strconv.Itoa(port),
		token:  token,
		client: &http.Client{Timeout: brokerTimeout},
	}, nil
}

// machineToken reads <dir>/orchestrator-token as a plain file, refusing the
// Swift app's directory however it is spelled. It never makes one: a missing
// token means no daemon has started with this directory.
func machineToken(dir string) (string, error) {
	if err := skillfile.RefuseForeign(dir, foreignDirs()...); err != nil {
		return "", err
	}
	path := filepath.Join(dir, devices.MachineTokenFile)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("no orchestrator token at %s — has a daemon started with this directory? "+
			"(CLAWDLINE_NEXT_DIR chooses it)", path)
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a plain file; it was not read", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, brokerTokenLimit+1))
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(data))
	if len(data) > brokerTokenLimit || token == "" || strings.ContainsAny(token, " \t\r\n") {
		return "", fmt.Errorf("%s does not hold a usable token; it was left as it is", path)
	}
	return token, nil
}

// answer is one response: its status, and its body with the token masked.
type answer struct {
	Status int
	Body   []byte
}

// refusal is the daemon's refusal envelope, when the body is one.
func (a answer) refusal() (code, message string) {
	var r contract.AuthRefusal
	if json.Unmarshal(a.Body, &r) == nil {
		return r.Error.Code, r.Error.Message
	}
	return "", ""
}

// ok is a 2xx answer.
func (a answer) ok() bool { return a.Status >= 200 && a.Status < 300 }

// request sends one request. body is JSON-encoded when it is not nil. A
// non-empty key goes out as Idempotency-Key.
func (b *broker) request(method, path string, query url.Values, body any, key string) (answer, error) {
	target := b.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return answer{}, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, target, reader)
	if err != nil {
		return answer{}, err
	}
	req.Header.Set("X-Clawdline-Orchestrator", b.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	res, err := b.client.Do(req)
	if err != nil {
		// The client's error names the URL, which carries no credential.
		return answer{}, fmt.Errorf("the daemon at %s did not answer: %w", b.base, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, brokerAnswerLimit+1))
	if err != nil {
		return answer{}, fmt.Errorf("the daemon's answer could not be read: %w", err)
	}
	if len(data) > brokerAnswerLimit {
		return answer{}, fmt.Errorf("the daemon's answer was larger than %d bytes", brokerAnswerLimit)
	}
	return answer{Status: res.StatusCode, Body: b.mask(data)}, nil
}

// mask replaces the token wherever it appears. The daemon never sends it
// back; this is so that a mistake there is not also a leak here.
func (b *broker) mask(data []byte) []byte {
	if b.token == "" {
		return data
	}
	return bytes.ReplaceAll(data, []byte(b.token), []byte("<orchestrator-token>"))
}

// newKey is a fresh Idempotency-Key.
func newKey(prefix string) string {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf)
	return prefix + "-" + hex.EncodeToString(buf)
}

// report prints an answer the way every thin command does: the body on
// stdout when the daemon said yes, and its code and message on stderr when it
// said no. It answers the exit status: 0 for yes, 1 for a refusal.
func report(stdout, stderr io.Writer, name string, a answer) int {
	if a.ok() {
		out := a.Body
		var pretty bytes.Buffer
		if json.Indent(&pretty, a.Body, "", "  ") == nil {
			out = pretty.Bytes()
		}
		_, _ = stdout.Write(out)
		if len(out) == 0 || out[len(out)-1] != '\n' {
			fmt.Fprintln(stdout)
		}
		return 0
	}
	code, message := a.refusal()
	if code == "" {
		fmt.Fprintf(stderr, "clawdline %s: the daemon answered %d: %s\n", name, a.Status,
			strings.TrimSpace(string(a.Body)))
		return 1
	}
	fmt.Fprintf(stderr, "clawdline %s: refused, %d %s: %s\n", name, a.Status, code, message)
	return 1
}
