// Package devices keeps who may reach this daemon on disk: the paired devices,
// the local token, the machine token and the audit log, each a file under this
// app's own directory.
//
// **Files, not a keychain, on purpose.** The daemon runs on macOS, Linux and
// Windows, and a file with an owner-only mode is the one store all three have.
// It is the same trust boundary the Swift app draws with
// ~/.config/clawdline/remote.json: anything running as this user can read it,
// and a web page cannot. The seam for an operating-system keychain is
// auth.Store plus MachineToken here — the device list holds only hashes and can
// stay a file; the local and machine tokens are the two plaintext secrets a
// keychain would take over. The local token is read by scripts, so even then
// something a script can read has to exist.
//
// The manners, all of them the Swift app's or stricter:
//
//   - The directory is 0700 and every file 0600, set on every write rather
//     than trusted from creation.
//   - A write goes to a temporary file in the same directory, is synced, and is
//     renamed over the old one. A reader sees the old file or the new one.
//   - Only a token's SHA-256 is in the device file.
//   - A device file that exists and cannot be read is a refusal, never an
//     empty list that the next save would write over.
//   - Never the Swift app's directory. This package does not open, read or
//     accept anything under ~/.config/clawdline.
package devices

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/auth"
)

const (
	// StoreFile is the device list, named as the Swift app names its own.
	StoreFile = "remote.json"
	// LocalTokenFile is this machine's own token, for the shell and scripts.
	LocalTokenFile = "local-token"
	// MachineTokenFile is the orchestrator credential: dispatch and every
	// write under /v1/orchestrator/.
	MachineTokenFile = "orchestrator-token"
	// AuditFile is one JSON object per line, append-only.
	AuditFile = "remote-audit.jsonl"

	machineTokenLimit = 512
)

// ErrForeignDir is the answer for a directory that is the Swift app's.
var ErrForeignDir = errors.New("refusing the Swift app's directory")

// ErrUnreadable is the answer for a device file that exists and is not one.
var ErrUnreadable = errors.New("the device file cannot be read")

// Files is one directory's worth of credentials. One per process per
// directory; its lock orders this process's writers.
type Files struct {
	dir string
	mu  sync.Mutex
}

// Open prepares dir, refusing it when it is, or resolves into, any of foreign.
func Open(dir string, foreign ...string) (*Files, error) {
	if dir == "" {
		return nil, errors.New("no state directory")
	}
	for _, f := range foreign {
		if f != "" && (within(dir, f) || within(resolved(dir), resolved(f))) {
			return nil, fmt.Errorf("%w: %s", ErrForeignDir, dir)
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// This app's own directory. Tightened even when it already existed: the
	// files in it are owner-only, and a readable directory still lists them.
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	f := &Files{dir: dir}
	for _, name := range []string{StoreFile, LocalTokenFile, MachineTokenFile, AuditFile} {
		if err := os.Chmod(f.path(name), 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return f, nil
}

// Dir is the directory these files live in.
func (f *Files) Dir() string { return f.dir }

func (f *Files) path(name string) string { return filepath.Join(f.dir, name) }

// LocalTokenPath is where the shell and scripts read this machine's token.
func (f *Files) LocalTokenPath() string { return f.path(LocalTokenFile) }

// MachineTokenPath is where a local orchestrator reads its credential.
func (f *Files) MachineTokenPath() string { return f.path(MachineTokenFile) }

// AuditPath is the audit log.
func (f *Files) AuditPath() string { return f.path(AuditFile) }

// The file's shape is the Swift app's remote.json, key for key, so one
// person's description of either is true of both.
type storedDevice struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Hash     string   `json:"hash"`
	Caps     []string `json:"caps"`
	Created  float64  `json:"created"`
	LastSeen *float64 `json:"last_seen,omitempty"`
	Approved bool     `json:"approved"`
	Local    bool     `json:"local"`
}

type storedPassword struct {
	Hash       string `json:"hash"`
	Salt       string `json:"salt"`
	Iterations int    `json:"iterations"`
}

type storedFile struct {
	Version  int             `json:"version"`
	Devices  []storedDevice  `json:"devices"`
	Password *storedPassword `json:"password,omitempty"`
}

// Load reads the device list. No file is an empty list; a file that is not
// the shape above is ErrUnreadable and is left as it is.
func (f *Files) Load() (auth.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := os.ReadFile(f.path(StoreFile))
	if errors.Is(err, os.ErrNotExist) {
		return auth.State{}, nil
	}
	if err != nil {
		return auth.State{}, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	var file storedFile
	if err := json.Unmarshal(data, &file); err != nil {
		return auth.State{}, fmt.Errorf("%w: %s is not a device list", ErrUnreadable, f.path(StoreFile))
	}
	var state auth.State
	for _, row := range file.Devices {
		if row.ID == "" || row.Hash == "" {
			continue
		}
		var caps []auth.Capability
		for _, c := range row.Caps {
			if cap, ok := auth.ParseCapability(c); ok {
				caps = append(caps, cap)
			}
		}
		d := auth.Device{
			ID: row.ID, Name: row.Name, Hash: row.Hash, Caps: auth.NewCaps(caps...),
			Created: fromUnix(row.Created), Approved: row.Approved, Local: row.Local,
		}
		if d.Name == "" {
			d.Name = "?"
		}
		if row.LastSeen != nil {
			d.LastSeen = fromUnix(*row.LastSeen)
		}
		state.Devices = append(state.Devices, d)
	}
	if p := file.Password; p != nil {
		hash, err1 := base64.StdEncoding.DecodeString(p.Hash)
		salt, err2 := base64.StdEncoding.DecodeString(p.Salt)
		if err1 != nil || err2 != nil {
			return auth.State{}, fmt.Errorf("%w: the password record is not base64", ErrUnreadable)
		}
		iterations := p.Iterations
		if iterations == 0 {
			iterations = auth.PasswordIterations
		}
		state.Password = &auth.Password{Hash: hash, Salt: salt, Iterations: iterations}
	}
	return state, nil
}

// Save writes the device list atomically.
func (f *Files) Save(state auth.State) error {
	file := storedFile{Version: 1, Devices: []storedDevice{}}
	for _, d := range state.Devices {
		caps := make([]string, 0, len(d.Caps))
		for _, c := range d.Caps {
			caps = append(caps, string(c))
		}
		sort.Strings(caps)
		row := storedDevice{
			ID: d.ID, Name: d.Name, Hash: d.Hash, Caps: caps,
			Created: toUnix(d.Created), Approved: d.Approved, Local: d.Local,
		}
		if !d.LastSeen.IsZero() {
			seen := toUnix(d.LastSeen)
			row.LastSeen = &seen
		}
		file.Devices = append(file.Devices, row)
	}
	if p := state.Password; p != nil {
		file.Password = &storedPassword{
			Hash:       base64.StdEncoding.EncodeToString(p.Hash),
			Salt:       base64.StdEncoding.EncodeToString(p.Salt),
			Iterations: p.Iterations,
		}
	}
	body, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writeAtomically(StoreFile, append(body, '\n'))
}

// ReadLocalToken returns the token file's contents as written.
func (f *Files) ReadLocalToken() (string, error) {
	data, err := os.ReadFile(f.path(LocalTokenFile))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteLocalToken replaces the token file.
func (f *Files) WriteLocalToken(token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writeAtomically(LocalTokenFile, []byte(token))
}

// MachineToken is the orchestrator credential, made only when there is none.
//
// An existing file that cannot be read, or does not hold a plausible token, is
// kept exactly as it is and answers "" — which verifies nothing — until
// somebody repairs it. Replacing it would silently lock out every caller that
// had read the old one; accepting it would be accepting something unknown.
func (f *Files) MachineToken() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := f.path(MachineTokenFile)
	data, err := os.ReadFile(path)
	if err == nil {
		token := strings.TrimSpace(string(data))
		if token == "" || len(token) > machineTokenLimit || !validText(token) {
			return "", fmt.Errorf("%s is not a usable token; it was left as it is", path)
		}
		return token, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%s is unreadable; it was left as it is: %w", path, err)
	}
	made, err := auth.NewToken(rand.Reader)
	if err != nil {
		return "", err
	}
	// Exclusive creation: a second process minting at the same moment loses
	// the race and reads the winner's token instead of replacing it.
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}
	if err != nil {
		return "", err
	}
	if _, err := out.WriteString(made); err != nil {
		_ = out.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	syncDir(f.dir)
	return made, nil
}

// Audit appends one line: the time, the event and the fields, keys sorted, as
// the Swift app writes it. The callers pass names and ids only; no token and
// no code is ever handed to this function.
//
// A failed append is logged and not returned. What it records has already
// happened, and failing the request would not undo it.
func (f *Files) Audit(event string, fields map[string]string) {
	row := map[string]any{"at": time.Now().Unix(), "event": event}
	for k, v := range fields {
		row[k] = v
	}
	var line strings.Builder
	enc := json.NewEncoder(&line)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(row); err != nil {
		log.Printf("audit: could not encode %s: %v", event, err)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out, err := os.OpenFile(f.path(AuditFile), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		log.Printf("audit: could not open %s: %v", f.path(AuditFile), err)
		return
	}
	defer out.Close()
	if _, err := out.WriteString(line.String()); err != nil {
		log.Printf("audit: could not append %s: %v", event, err)
	}
}

func (f *Files) writeAtomically(name string, body []byte) (err error) {
	tmp, err := os.CreateTemp(f.dir, "."+name+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()
	if err = tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err = tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmpName, f.path(name)); err != nil {
		return err
	}
	syncDir(f.dir)
	return nil
}

// syncDir makes a rename durable. A failure is not a failed write: the new
// contents are already the file.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

func validText(s string) bool {
	for _, r := range s {
		if r < 0x21 || r == 0x7f {
			return false
		}
	}
	return true
}

func toUnix(t time.Time) float64 {
	if t.IsZero() {
		return 0
	}
	return float64(t.UnixNano()) / 1e9
}

func fromUnix(v float64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	sec := int64(v)
	return time.Unix(sec, int64((v-float64(sec))*1e9))
}

// within reports whether path is dir or inside it, comparing cleaned absolute
// spellings.
func within(path, dir string) bool {
	p, err1 := filepath.Abs(path)
	d, err2 := filepath.Abs(dir)
	if err1 != nil || err2 != nil {
		// A path that cannot be made absolute cannot be shown to be elsewhere.
		return true
	}
	return p == d || strings.HasPrefix(p, d+string(filepath.Separator))
}

// resolved follows symlinks as far as the path exists, so a directory that
// does not exist yet still resolves through the parents that do.
func resolved(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	rest := ""
	for cur := abs; ; {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}
