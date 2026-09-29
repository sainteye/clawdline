package devices

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// GrantsFile is which paired devices may see and operate this machine's
// terminals (plan v3 D5). It sits beside the device file and is not part of
// it, on purpose.
//
// **The device file decodes strictly** (deviceIn.device): a capability it does
// not know makes the whole file unreadable, and an unreadable device file is a
// daemon that answers 503 to every route behind the gate, this machine's own
// token included. A `terminal` capability written there would have made every
// older daemon — a downgrade, a restore — lock its owner out. An older daemon
// never opens this file, so a grant written here costs it nothing.
//
// Its failure is the terminal feature's alone: a file that cannot be read
// grants nobody (Granted answers false with the reason), and nothing else
// reads it.
const GrantsFile = "terminal-grants.json"

// MaxGrantsBytes is the most of the grants file this reads. One row is a
// device id of at most 128 bytes and a time; the device list holds at most a
// few hundred devices (`devices.list`), which is well under this. A larger
// file is refused, not cut (the `terminal.grants_bytes` capacity row).
const MaxGrantsBytes = 256 << 10

// Grant is one device's terminal grant.
type Grant struct {
	GrantedAt time.Time
}

type grantRow struct {
	GrantedAt *float64 `json:"granted_at"`
}

// Grants reads and writes GrantsFile through the same Files that guards the
// device file: opened only as a plain file, written by an atomic replace at
// 0600. It holds the last reading and reads the file again when its size or
// time moved, so a hand edit counts at the next question and a question costs
// one stat.
type Grants struct {
	files *Files

	mu      sync.Mutex
	rows    map[string]Grant
	err     error
	stamp   grantsStamp
	changed func()
}

type grantsStamp struct {
	exists bool
	size   int64
	mod    time.Time
}

// OpenGrants is the grants beside files. It never fails: a file that cannot
// be read is an answer (Err), not a refusal to start.
func OpenGrants(files *Files) *Grants {
	g := &Grants{files: files}
	g.mu.Lock()
	g.refresh()
	g.mu.Unlock()
	return g
}

// OnChange is called after every write, whatever it changed. The daemon hands
// it auth.Authority.NotifyChanged, so an open terminal stream is swept the
// moment a grant is taken away.
func (g *Grants) OnChange(fn func()) {
	g.mu.Lock()
	g.changed = fn
	g.mu.Unlock()
}

// Granted is whether device holds a terminal grant. A file that cannot be
// read grants nobody, and err says why.
func (g *Grants) Granted(device string) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.refresh()
	if g.err != nil {
		return false, g.err
	}
	_, ok := g.rows[device]
	return ok, nil
}

// All is every grant, or the reason there are none to be read.
func (g *Grants) All() (map[string]Grant, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.refresh()
	if g.err != nil {
		return nil, g.err
	}
	out := make(map[string]Grant, len(g.rows))
	for k, v := range g.rows {
		out[k] = v
	}
	return out, nil
}

// Err is why the file could not be read, or nil.
func (g *Grants) Err() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.refresh()
	return g.err
}

// Set grants device, or takes its grant away. Granting a device that already
// holds one keeps its first time. A file that could not be read is not
// overwritten: whatever it held would be lost, so the change is refused with
// the reason until a person repairs or removes the file.
func (g *Grants) Set(device string, grant bool, now time.Time) (Grant, error) {
	if device == "" || len(device) > idLimit || !validText(device) {
		return Grant{}, errors.New("that is not a device id")
	}
	g.mu.Lock()
	g.refresh()
	if g.err != nil {
		err := g.err
		g.mu.Unlock()
		return Grant{}, fmt.Errorf("the terminal grants could not be read, so they were not changed: %w", err)
	}
	next := make(map[string]Grant, len(g.rows)+1)
	for k, v := range g.rows {
		next[k] = v
	}
	if grant {
		if _, ok := next[device]; !ok {
			next[device] = Grant{GrantedAt: now}
		}
	} else {
		delete(next, device)
	}
	if err := g.write(next); err != nil {
		g.mu.Unlock()
		return Grant{}, err
	}
	out := next[device]
	fn := g.changed
	g.mu.Unlock()
	if fn != nil {
		fn()
	}
	return out, nil
}

// Keep removes every grant whose device is not in held, and is how a revoked
// device loses its grant: the routes that revoke call it with the devices
// that are left. It writes only when it removes something.
func (g *Grants) Keep(held func(device string) bool) error {
	g.mu.Lock()
	g.refresh()
	if g.err != nil {
		g.mu.Unlock()
		return g.err
	}
	next := map[string]Grant{}
	for k, v := range g.rows {
		if held(k) {
			next[k] = v
		}
	}
	if len(next) == len(g.rows) {
		g.mu.Unlock()
		return nil
	}
	err := g.write(next)
	fn := g.changed
	g.mu.Unlock()
	if err == nil && fn != nil {
		fn()
	}
	return err
}

// refresh reads the file again when it moved since the last reading. With
// g.mu held.
func (g *Grants) refresh() {
	info, err := os.Lstat(g.files.path(GrantsFile))
	stamp := grantsStamp{}
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		g.rows, g.err, g.stamp = nil, err, grantsStamp{}
		return
	default:
		stamp = grantsStamp{exists: true, size: info.Size(), mod: info.ModTime()}
	}
	if g.rows != nil && g.err == nil && stamp == g.stamp {
		return
	}
	g.stamp = stamp
	if !stamp.exists {
		g.rows, g.err = map[string]Grant{}, nil
		return
	}
	data, err := g.files.read(GrantsFile, MaxGrantsBytes)
	if err != nil {
		g.rows, g.err = nil, err
		return
	}
	rows, err := decodeGrants(data)
	if err != nil {
		g.rows, g.err = nil, fmt.Errorf("%s: %w", g.files.path(GrantsFile), err)
		return
	}
	g.rows, g.err = rows, nil
}

func (g *Grants) write(rows map[string]Grant) error {
	body, err := encodeGrants(rows)
	if err != nil {
		return err
	}
	if len(body) > MaxGrantsBytes {
		return fmt.Errorf("the terminal grants would be larger than the %d bytes the daemon reads", MaxGrantsBytes)
	}
	if err := g.files.writeAtomically(GrantsFile, body); err != nil {
		return err
	}
	g.rows, g.err = rows, nil
	// The next question stats the file it just wrote and reads it once more;
	// that is the price of a hand edit counting, and it is one read.
	g.stamp = grantsStamp{}
	return nil
}

// decodeGrants is the file as `{device_id: {granted_at}}`: every key an id
// the device file could hold, every row with its time and nothing else.
// Anything else is a file this did not write, and it grants nobody.
func decodeGrants(data []byte) (map[string]Grant, error) {
	var raw map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("not a JSON object: %w", err)
	}
	if raw == nil {
		return nil, errors.New("not a JSON object")
	}
	out := make(map[string]Grant, len(raw))
	for id, body := range raw {
		if id == "" || len(id) > idLimit || !validText(id) {
			return nil, errors.New("a key is not a device id")
		}
		var row grantRow
		d := json.NewDecoder(bytes.NewReader(body))
		d.DisallowUnknownFields()
		if err := d.Decode(&row); err != nil {
			return nil, fmt.Errorf("device %q: %w", id, err)
		}
		if row.GrantedAt == nil || *row.GrantedAt < 0 {
			return nil, fmt.Errorf("device %q has no granted_at", id)
		}
		out[id] = Grant{GrantedAt: fromUnix(*row.GrantedAt)}
	}
	return out, nil
}

func encodeGrants(rows map[string]Grant) ([]byte, error) {
	ids := make([]string, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b bytes.Buffer
	b.WriteString("{")
	for i, id := range ids {
		key, err := json.Marshal(id)
		if err != nil {
			return nil, err
		}
		at := toUnix(rows[id].GrantedAt)
		row, err := json.Marshal(grantRow{GrantedAt: &at})
		if err != nil {
			return nil, err
		}
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("\n  ")
		b.Write(key)
		b.WriteString(": ")
		b.Write(row)
	}
	if len(ids) > 0 {
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return b.Bytes(), nil
}

// Reading is the `terminal.grants_bytes` row: the file's size, or why it
// could not be read.
func (g *Grants) Reading() capacity.Reading {
	info, err := os.Lstat(g.files.path(GrantsFile))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return capacity.Reading{Known: true, Note: "no device holds a terminal grant"}
	case err != nil:
		return capacity.Reading{Err: err.Error()}
	}
	r := capacity.Reading{Known: true, Used: info.Size()}
	if err := g.Err(); err != nil {
		r.Err = err.Error()
	}
	return r
}
