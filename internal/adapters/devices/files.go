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
//   - A device file that exists and cannot be read — or reads and is not
//     exactly what Save writes — is a refusal, never an empty list that the
//     next save would write over.
//   - A file is opened as the file it is, never through a symlink. A link in
//     place of any of the four stops the daemon at startup, and one put there
//     later is refused where it is met; nothing is tightened, read or appended
//     through one.
//   - Never the Swift app's directory. This package does not open, read or
//     accept anything under ~/.config/clawdline.
package devices

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline-go/internal/domain/auth"
	"github.com/sainteye/clawdline-go/internal/domain/capacity"
)

const (
	// StoreFile is the device list, named as the Swift app names its own.
	StoreFile = "remote.json"
	// LocalTokenFile is this machine's own token, for the shell and scripts.
	LocalTokenFile = "local-token"
	// MachineTokenFile is the orchestrator credential: dispatch and every
	// write under /v1/orchestrator/.
	MachineTokenFile = "orchestrator-token"
	// AuditFile is one JSON object per line, append-only. At the capacity
	// register's `audit.security` size it is closed and renamed to a segment,
	// AuditSegmentPrefix + a UTC time + ".jsonl", and a new one is begun.
	// Segments are never deleted here: only a person removes one.
	AuditFile = "remote-audit.jsonl"
	// AuditSegmentPrefix begins every closed segment's name, so that
	// `ls remote-audit.*` is the whole audit, current file included.
	AuditSegmentPrefix = "remote-audit."

	machineTokenLimit = 512
	// storeVersion is the device file's version, the one Save writes and the
	// only one Load reads.
	storeVersion = 1
	// The most any file here is read: far past what the file holds, short of
	// what a mistake could make of it.
	storeLimit = 4 << 20
	tokenLimit = 4 << 10
	// idLimit and storedNameLimit bound what a device row may carry.
	idLimit         = 128
	storedNameLimit = 256
	// auditFieldLimit is the most of any one value an audit line keeps.
	// Callers already pass short names; this holds whatever they pass.
	auditFieldLimit = 256
)

// credentialFiles are the four files this package keeps.
var credentialFiles = []string{StoreFile, LocalTokenFile, MachineTokenFile, AuditFile}

// ErrForeignDir is the answer for a directory that is the Swift app's.
var ErrForeignDir = errors.New("refusing the Swift app's directory")

// ErrUnreadable is the answer for a device file that exists and is not one.
var ErrUnreadable = errors.New("the device file cannot be read")

// ErrNotRegular is the answer for one of the four files when it is a symlink,
// a directory, or anything else but a plain file.
var ErrNotRegular = errors.New("not a plain file")

// Files is one directory's worth of credentials. One per process per
// directory; its lock orders this process's writers.
type Files struct {
	dir string
	mu  sync.Mutex

	// The audit's segment size, and what its writer has done since this
	// process opened the directory. All under mu.
	auditLimit   int64
	rotations    int64
	auditErrors  int64
	lastRotation time.Time
	// appendErr and rotateErr are the last append's and the last rotation's
	// failure, nil once one succeeds. Either one means a security event may
	// not be where it should be, which the register reports as exhausted.
	appendErr error
	rotateErr error
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
	// Each file is tightened through its own descriptor, opened as the file it
	// is: a link called remote.json is somebody else's file, and a chmod by
	// path would have been done to whatever it points at.
	for _, name := range credentialFiles {
		file, err := f.open(name, os.O_RDONLY, 0)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		err = file.Chmod(0o600)
		_ = file.Close()
		if err != nil {
			return nil, err
		}
	}
	return f, nil
}

// open opens one of this directory's files as the file it is and only when it
// is a plain file, else ErrNotRegular. The Lstat before refuses a link that is
// there; O_NOFOLLOW refuses one that appears after it; the same-file check
// after refuses a swap. With os.O_CREATE a missing file is made exclusively,
// which does not follow a link either; without it, a missing file is
// os.ErrNotExist.
func (f *Files) open(name string, flag int, perm os.FileMode) (*os.File, error) {
	path := f.path(name)
	for range 2 {
		before, err := os.Lstat(path)
		exists := err == nil
		switch {
		case exists && !before.Mode().IsRegular():
			return nil, fmt.Errorf("%w: %s is %s", ErrNotRegular, path, kindOf(before.Mode()))
		case !exists && !(errors.Is(err, os.ErrNotExist) && flag&os.O_CREATE != 0):
			return nil, err
		}
		mode := flag &^ (os.O_CREATE | os.O_EXCL)
		if !exists {
			mode = flag | os.O_EXCL
		}
		file, err := os.OpenFile(path, mode|noFollow, perm)
		if !exists && errors.Is(err, os.ErrExist) {
			// Made by somebody else between the two looks: look again.
			continue
		}
		if err != nil {
			if isLinkRefusal(err) {
				return nil, fmt.Errorf("%w: %s became a symlink", ErrNotRegular, path)
			}
			return nil, err
		}
		after, err := file.Stat()
		if err == nil && (!after.Mode().IsRegular() || exists && !os.SameFile(before, after)) {
			err = fmt.Errorf("%w: %s changed while it was being opened", ErrNotRegular, path)
		}
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		return file, nil
	}
	return nil, fmt.Errorf("%w: %s kept changing while it was being opened", ErrNotRegular, path)
}

// read is a whole file, opened as open opens it, and refused past limit bytes.
func (f *Files) read(name string, limit int64) ([]byte, error) {
	file, err := f.open(name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", f.path(name), limit)
	}
	return data, nil
}

func kindOf(m os.FileMode) string {
	switch {
	case m&os.ModeSymlink != 0:
		return "a symlink"
	case m.IsDir():
		return "a directory"
	default:
		return "not a plain file"
	}
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
// exactly the shape above is ErrUnreadable and is left as it is.
func (f *Files) Load() (auth.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := f.read(StoreFile, storeLimit)
	if errors.Is(err, os.ErrNotExist) {
		return auth.State{}, nil
	}
	if err != nil {
		return auth.State{}, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	state, err := decodeStore(data)
	if err != nil {
		return auth.State{}, fmt.Errorf("%w: %s is not a device list: %v", ErrUnreadable, f.path(StoreFile), err)
	}
	return state, nil
}

// The file as it is read. Every key is a pointer, so a key that is missing is
// told apart from one that is zero.
type fileIn struct {
	Version  *int        `json:"version"`
	Devices  *[]deviceIn `json:"devices"`
	Password *passwordIn `json:"password"`
}

type deviceIn struct {
	ID       *string   `json:"id"`
	Name     *string   `json:"name"`
	Hash     *string   `json:"hash"`
	Caps     *[]string `json:"caps"`
	Created  *float64  `json:"created"`
	LastSeen *float64  `json:"last_seen"`
	Approved *bool     `json:"approved"`
	Local    *bool     `json:"local"`
}

type passwordIn struct {
	Hash       *string `json:"hash"`
	Salt       *string `json:"salt"`
	Iterations *int    `json:"iterations"`
}

// decodeStore is the one reading of the device file, and it is strict: one
// JSON object, version 1, the keys Save writes and no others, each of its
// type, and nothing after it. A file that is anything else was not written by
// this app, and a file this app did not write is not one it acts on — or,
// through its next save, writes over. What the rows mean (one id each, real
// digests, one local device, the password's sizes and cost) is the auth
// package's to check.
func decodeStore(data []byte) (auth.State, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var file *fileIn
	if err := dec.Decode(&file); err != nil {
		return auth.State{}, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return auth.State{}, errors.New("something follows the object")
	}
	switch {
	case file == nil:
		return auth.State{}, errors.New("it is null")
	case file.Version == nil:
		return auth.State{}, errors.New("it has no version")
	case *file.Version != storeVersion:
		return auth.State{}, fmt.Errorf("it is version %d, and this app reads version %d", *file.Version, storeVersion)
	case file.Devices == nil:
		return auth.State{}, errors.New("it has no devices array")
	}
	var state auth.State
	for i, row := range *file.Devices {
		d, err := row.device()
		if err != nil {
			return auth.State{}, fmt.Errorf("device %d: %v", i, err)
		}
		state.Devices = append(state.Devices, d)
	}
	if file.Password != nil {
		pw, err := file.Password.password()
		if err != nil {
			return auth.State{}, fmt.Errorf("password: %v", err)
		}
		state.Password = pw
	}
	return state, nil
}

func (row deviceIn) device() (auth.Device, error) {
	var missing []string
	for key, absent := range map[string]bool{
		"id": row.ID == nil, "name": row.Name == nil, "hash": row.Hash == nil,
		"caps": row.Caps == nil, "created": row.Created == nil,
		"approved": row.Approved == nil, "local": row.Local == nil,
	} {
		if absent {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return auth.Device{}, fmt.Errorf("no %s", strings.Join(missing, ", "))
	}
	if *row.ID == "" || len(*row.ID) > idLimit || !validText(*row.ID) {
		return auth.Device{}, errors.New("the id is not an id")
	}
	if len(*row.Name) > storedNameLimit {
		return auth.Device{}, fmt.Errorf("the name is longer than %d bytes", storedNameLimit)
	}
	if len(*row.Caps) == 0 {
		return auth.Device{}, errors.New("no capabilities")
	}
	var caps []auth.Capability
	for _, c := range *row.Caps {
		cap, ok := auth.ParseCapability(c)
		if !ok {
			return auth.Device{}, fmt.Errorf("unknown capability %q", c)
		}
		caps = append(caps, cap)
	}
	if *row.Created < 0 || row.LastSeen != nil && *row.LastSeen < 0 {
		return auth.Device{}, errors.New("a time before 1970")
	}
	d := auth.Device{
		ID: *row.ID, Name: *row.Name, Hash: *row.Hash, Caps: auth.NewCaps(caps...),
		Created: fromUnix(*row.Created), Approved: *row.Approved, Local: *row.Local,
	}
	if d.Name == "" {
		d.Name = "?"
	}
	if row.LastSeen != nil {
		d.LastSeen = fromUnix(*row.LastSeen)
	}
	return d, nil
}

func (p passwordIn) password() (*auth.Password, error) {
	if p.Hash == nil || p.Salt == nil || p.Iterations == nil {
		return nil, errors.New("it needs hash, salt and iterations")
	}
	hash, err1 := base64.StdEncoding.DecodeString(*p.Hash)
	salt, err2 := base64.StdEncoding.DecodeString(*p.Salt)
	if err1 != nil || err2 != nil {
		return nil, errors.New("the hash or the salt is not base64")
	}
	return &auth.Password{Hash: hash, Salt: salt, Iterations: *p.Iterations}, nil
}

// Save writes the device list atomically.
func (f *Files) Save(state auth.State) error {
	file := storedFile{Version: storeVersion, Devices: []storedDevice{}}
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
	data, err := f.read(LocalTokenFile, tokenLimit)
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
	data, err := f.read(MachineTokenFile, tokenLimit)
	if err == nil {
		return usableMachineToken(path, data)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%s is unreadable; it was left as it is: %w", path, err)
	}
	made, err := auth.NewToken(rand.Reader)
	if err != nil {
		return "", err
	}
	// Exclusive creation, which never follows a link: a second process minting
	// at the same moment loses the race and reads the winner's token, with the
	// same care as any other read, instead of replacing it.
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|noFollow, 0o600)
	if errors.Is(err, os.ErrExist) {
		data, err := f.read(MachineTokenFile, tokenLimit)
		if err != nil {
			return "", fmt.Errorf("%s is unreadable; it was left as it is: %w", path, err)
		}
		return usableMachineToken(path, data)
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

// usableMachineToken is the file's token when it is one, and otherwise the
// reason it is not.
func usableMachineToken(path string, data []byte) (string, error) {
	token := strings.TrimSpace(string(data))
	if token == "" || len(token) > machineTokenLimit || !validText(token) {
		return "", fmt.Errorf("%s is not a usable token; it was left as it is", path)
	}
	return token, nil
}

// Audit appends one line: the time, the event and the fields, keys sorted, as
// the Swift app writes it. The callers pass names and ids only; no token and
// no code is ever handed to this function. Each value is cut to
// auditFieldLimit bytes, whatever the caller passed.
//
// A file already at its segment size is rotated first, so a line is never
// split across two segments and a segment runs past the size by at most the
// one line that reached it. A rotation that fails does not cost the line: it
// is appended to the file as it is, and the failure is counted and reported
// (AuditReading), because a security event not written is worse than a
// segment that is too long.
//
// A failed append is logged and not returned. What it records has already
// happened, and failing the request would not undo it. It is counted, and the
// register turns it into ok:false on /v1/health.
func (f *Files) Audit(event string, fields map[string]string) {
	row := map[string]any{"at": time.Now().Unix(), "event": event}
	for k, v := range fields {
		row[k] = clip(v, auditFieldLimit)
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
	f.rotateAudit()
	out, err := f.open(AuditFile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		f.auditFailed(&f.appendErr, err)
		log.Printf("audit: could not open %s: %v", f.path(AuditFile), err)
		return
	}
	defer out.Close()
	if _, err := out.WriteString(line.String()); err != nil {
		f.auditFailed(&f.appendErr, err)
		log.Printf("audit: could not append %s: %v", event, err)
		return
	}
	f.appendErr = nil
}

// SetAuditLimit is the capacity override for `audit.security`: the size at
// which the audit file becomes a segment. Zero or less is the register's
// default.
func (f *Files) SetAuditLimit(n int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auditLimit = n
}

func (f *Files) auditSegmentLimit() int64 {
	if f.auditLimit > 0 {
		return f.auditLimit
	}
	return capacity.Default(capacity.AuditSecurity)
}

// rotateAudit renames the audit file to a new segment when it has reached its
// size. Under mu. Nothing that is not a plain file is renamed, and a segment
// name that exists is never written over.
func (f *Files) rotateAudit() {
	path := f.path(AuditFile)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("%w: %s is %s", ErrNotRegular, path, kindOf(info.Mode()))
	}
	if err != nil {
		f.auditFailed(&f.rotateErr, err)
		log.Printf("audit: could not rotate: %v", err)
		return
	}
	if info.Size() < f.auditSegmentLimit() {
		return
	}
	now := time.Now().UTC()
	segment := f.path(AuditSegmentPrefix + now.Format("20060102T150405.000000000Z") + ".jsonl")
	if _, err := os.Lstat(segment); !errors.Is(err, os.ErrNotExist) {
		err = fmt.Errorf("segment %s already exists", segment)
		f.auditFailed(&f.rotateErr, err)
		log.Printf("audit: could not rotate: %v", err)
		return
	}
	if err := os.Rename(path, segment); err != nil {
		f.auditFailed(&f.rotateErr, err)
		log.Printf("audit: could not rotate %s: %v", path, err)
		return
	}
	syncDir(f.dir)
	f.rotateErr = nil
	f.rotations++
	f.lastRotation = now
	log.Printf("audit: %s reached %d bytes and is now %s", AuditFile, info.Size(), filepath.Base(segment))
}

func (f *Files) auditFailed(slot *error, err error) {
	*slot = err
	f.auditErrors++
}

// AuditReading is the `audit.security` row: the current segment's size, and
// what the writer has done. A stat, nothing read.
func (f *Files) AuditReading() capacity.Reading {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := capacity.Reading{Counters: capacity.Counters{
		Rotated: f.rotations, WriteErrors: f.auditErrors, LastActionAt: f.lastRotation,
	}}
	switch {
	case f.appendErr != nil:
		r.Failing, r.Note = true, "the last audit line was not written: "+f.appendErr.Error()
	case f.rotateErr != nil:
		r.Failing, r.Note = true, "the last rotation failed: "+f.rotateErr.Error()
	}
	info, err := os.Lstat(f.path(AuditFile))
	switch {
	case errors.Is(err, os.ErrNotExist):
		r.Known = true
	case err != nil:
		r.Err = err.Error()
	case !info.Mode().IsRegular():
		r.Err = fmt.Sprintf("%s is %s", f.path(AuditFile), kindOf(info.Mode()))
	default:
		r.Known, r.Used = true, info.Size()
	}
	return r
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

// clip is s cut to at most limit bytes, at a character boundary.
func clip(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
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
