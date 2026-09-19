// Package tunnel is the way in from outside: a cloudflared the person installed
// themselves, run as a child of this daemon, dialling out to Cloudflare so that
// nothing on this machine listens for the internet and no router is told
// anything.
//
// It is a port of the Swift app's RemoteTunnel.swift, rule for rule, with the
// defects that file carried left behind (see Supervisor). This file holds the
// rules as pure functions — which mode, whether to refuse, the command line,
// the configuration file, and the three readers of cloudflared's output —
// because this is where the bugs live, and a test that has to open a tunnel to
// Cloudflare to find one is a test nobody runs.
//
// **cloudflared is never bundled and never downloaded.** It is the person's own
// install, like tmux and whisper-cli: found where package managers put it,
// reported by name when it is missing.
//
// **It refuses to start while nothing is paired, and that refusal is the point
// of the package.** What is behind a tunnel is every repository, branch and task
// title on this machine and, through the transcript route, the conversations
// themselves. The gate already asks every request for a credential; the refusal
// here is the second, independent key: one config value must never be the
// whole distance between private and public.
package tunnel

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Mode is which way out of the machine — and Off is the one that ships.
type Mode string

const (
	Off   Mode = "off"
	Quick Mode = "quick"
	Named Mode = "named"
)

// ParseMode reads `remote_tunnel`. Anything unrecognised is Off: a typo in a
// config file must not be a config file that opens a tunnel.
func ParseMode(configured string) Mode {
	switch m := Mode(strings.ToLower(strings.TrimSpace(configured))); m {
	case Quick, Named:
		return m
	}
	return Off
}

// Plan is everything one launch is a function of. Comparable, so that a
// settings write which changed nothing about the tunnel leaves a running one
// alone: restarting a quick tunnel throws its address away and hands the
// person a new one.
type Plan struct {
	Mode     Mode
	Name     string
	Hostname string
	Port     int
	// Binary is the cloudflared that runs it, resolved before the launch.
	Binary string
}

// The limits the readers below keep. None of them is a store: each bounds one
// line or one sentence (internal/domain/capacity/testdata/baseline.txt).
const (
	// lineLimit is the longest run of cloudflared's output without a newline
	// that is read as a line. Something writing more than that without one is
	// not a cloudflared this package understands, and a string that grows
	// without end is not where that should be found out. The Swift app's
	// figure.
	lineLimit = 64 << 10
	// complaintLimit is the longest complaint kept: long enough to name a
	// cause, short enough to sit on a settings card.
	complaintLimit = 200
	// nameLimit is the longest tunnel name accepted. cloudflared's own names
	// are far shorter; this only stops a hand edit from being a paragraph.
	nameLimit = 64
)

// Hostname reads `remote_hostname` the one way both the tunnel and the gate
// read it: surrounding space and a scheme taken off, a trailing slash or dot
// taken off, lower case. ok is false when what is left is not a hostname — and
// an empty setting is ok, with nothing in it.
//
// The Swift app took `https://` off and wrote the rest into cloudflared's YAML
// as it stood. A hand-edited config.json does not pass through the settings
// route's check, and this value is written into a file cloudflared parses, so
// it is checked here, where it is used.
func Hostname(raw string) (string, bool) {
	h := strings.TrimSpace(raw)
	lower := strings.ToLower(h)
	for _, scheme := range []string{"https://", "http://"} {
		if strings.HasPrefix(lower, scheme) {
			h = h[len(scheme):]
			break
		}
	}
	h = strings.ToLower(strings.TrimRight(h, "/."))
	if h == "" {
		return "", true
	}
	return h, validHostname(h)
}

func validHostname(s string) bool {
	if len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r == '-' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
				return false
			}
		}
	}
	return true
}

// namePattern is a tunnel's name or id as this package will pass it: it goes
// on the command line after `run` and into the YAML after `tunnel:`, so it
// starts with a letter or digit (never a flag) and holds nothing a YAML reader
// could take for structure.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidName reports whether a tunnel name may be passed to cloudflared.
func ValidName(name string) bool { return len(name) <= nameLimit && namePattern.MatchString(name) }

// Refusal is every reason not to open a tunnel, in one place, as a function of
// its inputs; "" means there is none. Pure because this is the part that must
// be right.
//
// `paired` is the gate's own answer to "is anybody but this machine let in" —
// a password, or an approved device that is not this machine's own token —
// read from the same authority the gate asks, at the moment of the launch.
//
// `remote` is the settings window's first switch on the Remote tab, "let a
// browser or your phone see your sessions". The Swift app refused while it was
// off because its local server was off and there was nothing to tunnel to.
// Here the daemon always answers on loopback, so that reason is gone; the
// switch is kept as what its hint says it is — the person's own yes to being
// reachable from anything but this machine — so a tunnel still takes two
// deliberate settings and a paired device.
func Refusal(mode Mode, paired, remote bool, name, hostname string, hostnameOK bool) string {
	if mode == Off {
		return ""
	}
	if !paired {
		return "Not opening a tunnel: this machine has no paired device yet, so anything " +
			"that found the address would meet only a door. Pair a device first."
	}
	if !remote {
		return "Remote access is off, so this machine offers its sessions to nothing but itself. " +
			"Turn on \"let a browser or your phone see your sessions\" first."
	}
	if !hostnameOK {
		return "remote_hostname is not a hostname: " + quoteShort(hostname) + "."
	}
	if mode == Named {
		if strings.TrimSpace(name) == "" {
			return "A named tunnel needs remote_tunnel_name: the name you gave it in `cloudflared tunnel create`."
		}
		if !ValidName(name) {
			return "remote_tunnel_name is not a tunnel name cloudflared can be given: " + quoteShort(name) + "."
		}
		if hostname == "" {
			return "A named tunnel needs remote_hostname — the address you routed to it. " +
				"cloudflared does not print it, so this end cannot work it out."
		}
	}
	return ""
}

func quoteShort(s string) string {
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return fmt.Sprintf("%q", s)
}

// ConfigName is the file this package writes for cloudflared, in the
// daemon's own state directory. Ours, not the person's.
const ConfigName = "cloudflared.yml"

// Arguments is the command line after the binary, as a pure function of the
// plan, so the flags and the reasons for them can be read and tested without
// launching anything.
//
// The order is cloudflared's usage — `tunnel [tunnel options] run [TUNNEL]` —
// so the options sit in front of `run`.
//
//   - `--config`: **never optional.** Without it cloudflared reads
//     ~/.cloudflared/config.yml, which belongs to whatever else the person runs
//     tunnels for. Its `tunnel:` key overrides the name on the command line, so
//     `tunnel run clawdline` quietly started somebody's production tunnel
//     instead; and its ingress list applies to a quick tunnel too, so a
//     well-written one ending in `http_status:404` answered 404 for every
//     request. Both were seen on a real machine.
//   - `--no-autoupdate`: an updating cloudflared replaces itself and restarts,
//     which from here is indistinguishable from the tunnel dying.
//   - `--metrics 127.0.0.1:0`: left alone it takes the first free port of
//     20241…20245 in order, and the machine most likely to have cloudflared is
//     the one already running it for something else.
//   - `--grace-period 2s`: the default is thirty seconds spent waiting for
//     requests to finish, and the event stream never finishes on purpose.
func Arguments(p Plan, configPath string) []string {
	args := []string{
		"tunnel",
		"--config", configPath,
		"--no-autoupdate",
		"--metrics", "127.0.0.1:0",
		"--grace-period", "2s",
	}
	switch p.Mode {
	case Quick:
		args = append(args, "--url", fmt.Sprintf("http://127.0.0.1:%d", p.Port))
	case Named:
		args = append(args, "run", p.Name)
	}
	return args
}

// ConfigFile is the file that goes with those arguments. Pure, so what is
// written can be tested.
//
// A quick tunnel gets an almost empty one: its address is made up per run and
// cannot be in an ingress list, so `--url` is the whole of its routing. A named
// tunnel gets its one mapping and a 404 for everything else, because a tunnel
// that answers for hostnames it was never asked about is an open proxy.
//
// Every value that came from a setting is written as a double-quoted YAML
// scalar, which is JSON's string syntax, so no setting can become structure.
func ConfigFile(p Plan, credentials string) string {
	var b strings.Builder
	b.WriteString("# Written by Clawdline. Edits here are overwritten every time it starts.\n")
	b.WriteString("#\n")
	b.WriteString("# It exists so that ~/.cloudflared/config.yml is never read for this: that file's\n")
	b.WriteString("# `tunnel:` key overrides the name on the command line, and its ingress list would\n")
	b.WriteString("# answer 404 for an address it has never heard of.\n")
	// A file of comments alone parses as null, and cloudflared complains about
	// an empty configuration in the one place somebody looks when a tunnel
	// does not come up. This line is true and changes nothing.
	b.WriteString("no-autoupdate: true\n")
	if p.Mode != Named {
		return b.String()
	}
	fmt.Fprintf(&b, "\ntunnel: %s\n", yamlString(p.Name))
	if credentials != "" {
		fmt.Fprintf(&b, "credentials-file: %s\n", yamlString(credentials))
	}
	b.WriteString("\ningress:\n")
	fmt.Fprintf(&b, "  - hostname: %s\n", yamlString(p.Hostname))
	fmt.Fprintf(&b, "    service: %s\n", yamlString(fmt.Sprintf("http://127.0.0.1:%d", p.Port)))
	b.WriteString("    originRequest:\n")
	// The event stream is meant never to end; without this a phone is cut off
	// on a timer, for no reason it could act on.
	b.WriteString("      connectTimeout: 30s\n")
	b.WriteString("  - service: http_status:404\n")
	return b.String()
}

func yamlString(s string) string {
	out, _ := json.Marshal(s)
	return string(out)
}

// QuickURL is the address a quick tunnel was given, out of one line of
// cloudflared's logging, or "".
//
// It arrives inside a banner on stderr, where cloudflared writes everything:
//
//	2026-08-18T09:31:17Z INF |  https://denied-franchise-william-jade.trycloudflare.com                                   |
//
// Matched by its suffix, trying every `https://` on the line, because the run
// prints two other addresses first — "the first URL in the output" would have
// published Cloudflare's terms of use as the address of this machine:
//
//	2026-08-18T09:31:14Z INF Thank you for trying Cloudflare Tunnel. … (https://www.cloudflare.com/website-terms/) …
func QuickURL(line string) string {
	rest := line
	for {
		at := strings.Index(rest, "https://")
		if at < 0 {
			return ""
		}
		run := rest[at:]
		end := len(run)
		for i, r := range run {
			if !(r < 0x80 && (r == '.' || r == '-' || r == ':' || r == '/' ||
				(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))) {
				end = i
				break
			}
		}
		text := strings.TrimRight(run[:end], "/")
		if strings.HasSuffix(text, ".trycloudflare.com") && len(text) > len("https://.trycloudflare.com") {
			return text
		}
		rest = rest[at+len("https://"):]
	}
}

// RegisteredConnection reports whether a line says a connection to
// Cloudflare's edge is live, and where it landed ("?" when the line names no
// location: whether the tunnel is up must not depend on a field that exists for
// logging).
//
//	2026-08-18T09:31:18Z INF Registered tunnel connection connIndex=0 connection=da4d66ef-… event=0 ip=2606:4700:a8::5 location=tpe01 protocol=quic
//
// **`unregistered` contains `registered`.** cloudflared prints "Unregistered
// tunnel connection" on the way down, and a plain substring test reads a
// tunnel closing as a tunnel opening.
func RegisteredConnection(line string) (string, bool) {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "unregistered") {
		return "", false
	}
	// Older builds put the words the other way round; the binary is the
	// person's, and Homebrew is not the only way it arrives.
	announced := strings.Contains(lower, "registered tunnel connection") ||
		(strings.Contains(lower, " registered ") && strings.Contains(lower, "connindex="))
	if !announced {
		return "", false
	}
	if loc := field("location=", line); loc != "" {
		return loc, true
	}
	return "?", true
}

// Complaint is the human half of an error, if this line is one; "" otherwise.
//
// cloudflared's log lines are `<time> <LEVEL> <message>` with three-letter
// levels, and only ERR and FTL are complaints. But the failure a person will
// actually hit — a typo in the tunnel name — never reaches the logger: it is a
// bare line on stderr, exit status 1,
//
//	error parsing tunnel ID: clawdline-no-such-tunnel is neither the ID nor the name of any of your tunnels
//
// which is why anything that is not a log line counts. Being wrong about that
// costs one extra line in the log; the tidier rule reported the commonest
// mistake as "cloudflared kept exiting (status 1)".
func Complaint(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}
	parts := strings.SplitN(trimmed, " ", 3)
	if len(parts) >= 2 {
		switch parts[1] {
		case "DBG", "INF", "WRN":
			return ""
		case "ERR", "FTL":
			if len(parts) < 3 {
				return ""
			}
			return clip(parts[2], complaintLimit)
		}
	}
	return clip(trimmed, complaintLimit)
}

// clip cuts s to at most n bytes without splitting a character.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut]
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// Redacted is an address with the part that is secret taken out:
// `https://….trycloudflare.com`. While a quick tunnel is up, holding its four
// words *is* the access.
func Redacted(url string) string {
	scheme := strings.Index(url, "://")
	if scheme < 0 {
		return url
	}
	host := url[scheme+3:]
	if slash := strings.IndexByte(host, '/'); slash >= 0 {
		host = host[:slash]
	}
	dot := strings.IndexByte(host, '.')
	if dot < 0 {
		return url
	}
	return url[:scheme+3] + "…" + host[dot:]
}

// field is `key=value` up to the next space; cloudflared's structured fields
// all have that shape.
func field(key, line string) string {
	at := strings.Index(line, key)
	if at < 0 {
		return ""
	}
	value := line[at+len(key):]
	if end := strings.IndexAny(value, " \t"); end >= 0 {
		value = value[:end]
	}
	return value
}

// Backoff is 1, 2, 4 … seconds, capped at a minute. Doubling because the two
// things that take a tunnel down — a laptop that has just woken, an edge
// refusing connections — come back on their own or not at all.
func Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	return time.Duration(math.Min(60, math.Pow(2, float64(attempt-1)))) * time.Second
}

// BinaryPath is cloudflared, if this machine has one: the configured path
// first, then where package managers put it, then PATH — last because a daemon
// started by launchd or systemd has the bare system PATH, and Homebrew is not
// in it. "" when there is none.
//
// A configured path must be absolute. It comes from `cloudflared_path` in
// config.json, which only a hand edit sets — the settings route does not take
// it, because it names a program this daemon will run.
func BinaryPath(configured, pathEnv string) string {
	if c := strings.TrimSpace(configured); c != "" && filepath.IsAbs(c) && executable(c) {
		return c
	}
	for _, p := range []string{
		"/opt/homebrew/bin/cloudflared", // Homebrew, Apple silicon
		"/usr/local/bin/cloudflared",    // Homebrew on Intel, and the .pkg
		"/usr/bin/cloudflared",
		"/opt/local/bin/cloudflared", // MacPorts
	} {
		if executable(p) {
			return p
		}
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		if p := filepath.Join(dir, "cloudflared"); executable(p) {
			return p
		}
	}
	return ""
}

func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}
