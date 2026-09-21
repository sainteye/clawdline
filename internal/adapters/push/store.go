package push

import (
	"bytes"
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

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// The on-disk half, whose manners are internal/adapters/cloudkeys's because
// they are the ones this repository has already argued about:
//
//   - The directory is 0700 and every file 0600, set on every write rather
//     than trusted from creation.
//   - A write goes to a temporary file in the same directory, is synced, and
//     is renamed over the old one. A reader sees the old state or the new one.
//   - A file is opened as the file it is, never through a symlink.
//   - Never the Swift app's directory. This package does not open, read or
//     accept anything under ~/.config/clawdline, and in particular it never
//     reads that app's push.json: the two apps' subscriptions are separate on
//     purpose, and a browser paired with one has not agreed to be told things
//     by the other.
//
// Two files rather than the Swift app's one, and the split is the point. The
// Swift app keeps the VAPID scalar and every subscription in the same
// push.json, so a subscriptions list that will not parse takes the identity
// with it and every paired phone silently stops buzzing. Here the key has a
// file of its own: a subscriptions file somebody corrupted costs the
// subscriptions, and nothing else.
const (
	// DirName is the subdirectory of this app's state directory that holds
	// push state.
	DirName = "push"
	// VAPIDKeyFile holds the 32-byte P-256 private scalar, base64, one line —
	// the same spelling cloudkeys writes its secrets in.
	VAPIDKeyFile = "vapid-p256-v1"
	// SubscriptionsFile holds the rows a browser handed over.
	SubscriptionsFile = "subscriptions.json"

	secretLimit        = 4 << 10
	subscriptionsLimit = 1 << 20
	// storeVersion is the `version` field of SubscriptionsFile.
	storeVersion = 1
)

// ErrForeignDir is the answer for a directory that is, or resolves into, the
// Swift app's.
var ErrForeignDir = errors.New("refusing the Swift app's directory")

// ErrNotRegular is the answer for a file that is a symlink, a directory, or
// anything else but a plain file.
var ErrNotRegular = errors.New("not a plain file")

// ErrUnreadable is the answer for a file that exists and cannot be read.
var ErrUnreadable = errors.New("the file cannot be read")

// ErrSubscriptionsFull is an Add that would store a subscription past the
// register's `push.subscriptions` limit. Nothing was written. A subscription is
// somebody's standing request to be told, so the daemon never makes room by
// dropping one: a person unsubscribes a device, or revokes it.
var ErrSubscriptionsFull = errors.New("subscriptions_full")

// ErrSubscriptionsTooLarge is a save whose file would be larger than load
// reads. Past that bound every push fails (limits N14), so it is not written.
var ErrSubscriptionsTooLarge = errors.New("the subscriptions file would be larger than the daemon reads")

// Store is this daemon's push state: one VAPID identity and the subscriptions
// browsers have handed over.
type Store struct {
	dir string
	mu  sync.Mutex
	// loaded is whether the subscriptions file has been read this run.
	loaded bool
	rows   map[string]Subscription
	key    VAPIDKey
	// keyRead is whether the key file has been consulted this run; a minted
	// key is written through immediately, so this never caches an absence.
	keyRead bool
	// Log is where a replaced identity and a dropped subscription are said out
	// loud. nil means the standard logger.
	Log func(format string, args ...any)

	// limit is the `push.subscriptions` override; refused and refusedAt count
	// the additions it turned away. Under mu.
	limit     int64
	refused   int64
	refusedAt time.Time
}

// Open prepares the push directory under root, refusing it when root is, or
// resolves into, any of foreign.
func Open(root string, foreign ...string) (*Store, error) {
	if root == "" {
		return nil, errors.New("no state directory")
	}
	for _, other := range foreign {
		if other != "" && (within(root, other) || within(resolved(root), resolved(other))) {
			return nil, fmt.Errorf("%w: %s", ErrForeignDir, root)
		}
	}
	dir := filepath.Join(root, DirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// Tightened even when it already existed: a readable directory still lists
	// the names of the files in it.
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, rows: map[string]Subscription{}}
	for _, name := range []string{VAPIDKeyFile, SubscriptionsFile} {
		file, err := s.open(name, os.O_RDONLY, 0)
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
	return s, nil
}

// Dir is where the push files live.
func (s *Store) Dir() string { return s.dir }

func (s *Store) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log(format, args...)
		return
	}
	log.Printf(format, args...)
}

// VAPIDKey is the signing key, made once and kept.
//
// It is only replaced when the stored scalar will not parse *as a scalar*, and
// that case is logged in the loudest terms available, because it silently
// unsubscribes every device: a push service will not accept a message for a
// subscription signed by a key that is not the one the browser named when it
// subscribed. A file that cannot be *read* — a symlink, an I/O error, a file
// too large — is a refusal and never a mint: that is cloudkeys's rule, and it
// is what stops a transient failure from throwing away an identity.
func (s *Store) VAPIDKey() (VAPIDKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keyRead && s.key.Valid() {
		return s.key, nil
	}
	seed, found, err := s.readSecret(VAPIDKeyFile, VAPIDSeedBytes)
	switch {
	case err != nil && errors.Is(err, ErrUnreadable) && found:
		// The bytes are there and they are not a key. This is the one case the
		// Swift app mints over, and for the same reason: every subscription
		// that was made against the lost key is already dead, so refusing
		// forever leaves a machine that can never notify anybody again.
		s.logf("push: the stored VAPID key will not parse — minting a new one, which " +
			"invalidates every existing subscription; they will have to subscribe again")
	case err != nil:
		return VAPIDKey{}, err
	case found:
		key, keyErr := VAPIDKeyFromSeed(seed)
		if keyErr == nil {
			s.key, s.keyRead = key, true
			return key, nil
		}
		s.logf("push: the stored VAPID key will not parse — minting a new one, which " +
			"invalidates every existing subscription; they will have to subscribe again")
	}

	made, err := NewVAPIDKey()
	if err != nil {
		return VAPIDKey{}, err
	}
	if err := s.writeAtomically(VAPIDKeyFile, append([]byte(base64.StdEncoding.EncodeToString(made.Seed())), '\n')); err != nil {
		return VAPIDKey{}, err
	}
	s.key, s.keyRead = made, true
	s.logf("push: minted a VAPID key pair into %s", filepath.Join(s.dir, VAPIDKeyFile))
	return made, nil
}

// HasVAPIDKey is whether an identity exists without minting one.
//
// It is the guard on the send path: a machine that has never had a browser
// subscribe should not be growing key material because a session changed state.
func (s *Store) HasVAPIDKey() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keyRead && s.key.Valid() {
		return true
	}
	_, found, err := s.readSecret(VAPIDKeyFile, VAPIDSeedBytes)
	return found && err == nil
}

// Subscriptions is every row, oldest first.
func (s *Store) Subscriptions() ([]Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return nil, err
	}
	return s.sorted(), nil
}

// ForDevice is every row one paired device created.
func (s *Store) ForDevice(device string) ([]Subscription, error) {
	rows, err := s.Subscriptions()
	if err != nil {
		return nil, err
	}
	var out []Subscription
	for _, row := range rows {
		if row.Device == device {
			out = append(out, row)
		}
	}
	return out, nil
}

// Add stores one subscription, replacing any row for the same endpoint or the
// same device.
//
// One row per physical device as well as per endpoint. A browser can replace an
// endpoint across a reinstall; keeping the old row then sends both the
// declarative message and a legacy notification to the same phone, and leaves
// the test button reporting two sends.
//
// A subscription that replaces none is refused at the register's limit with
// ErrSubscriptionsFull, and nothing changes. One that replaces a row never is:
// the count does not grow.
func (s *Store) Add(subscription Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return err
	}
	next := make(map[string]Subscription, len(s.rows)+1)
	for id, existing := range s.rows {
		if existing.Endpoint == subscription.Endpoint || existing.Device == subscription.Device {
			continue
		}
		next[id] = existing
	}
	next[subscription.ID] = subscription
	if limit := s.subscriptionLimit(); int64(len(next)) > limit && len(next) > len(s.rows) {
		s.refused++
		s.refusedAt = time.Now()
		return fmt.Errorf("%w: %d subscriptions, the limit is %d", ErrSubscriptionsFull, len(next), limit)
	}
	previous := s.rows
	s.rows = next
	if err := s.save(); err != nil {
		s.rows = previous
		return err
	}
	return nil
}

// SetLimit is the capacity override for `push.subscriptions`. Zero or less
// is the register's default.
func (s *Store) SetLimit(n int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limit = n
}

func (s *Store) subscriptionLimit() int64 {
	if s.limit > 0 {
		return s.limit
	}
	return capacity.Default(capacity.PushSubscriptions)
}

// Reading is the `push.subscriptions` row: how many subscriptions are stored,
// and the additions refused. The file is read once per run, so after the
// first reading this is a length.
func (s *Store) Reading() capacity.Reading {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return capacity.Unmeasured(err.Error())
	}
	return capacity.Reading{Known: true, Used: int64(len(s.rows)),
		Counters: capacity.Counters{Refused: s.refused, LastActionAt: s.refusedAt}}
}

// Remove drops one subscription by id. A row that was not there is not an
// error: unsubscribing twice is what a reload of the page looks like.
func (s *Store) Remove(id string) (Subscription, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return Subscription{}, false, err
	}
	gone, ok := s.rows[id]
	if !ok {
		return Subscription{}, false, nil
	}
	delete(s.rows, id)
	return gone, true, s.save()
}

// RemoveDevice drops every subscription one paired device created, which is
// what revoking that device has to do: a browser that may no longer read this
// machine must not keep being told what it is doing.
func (s *Store) RemoveDevice(device string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return 0, err
	}
	removed := 0
	for id, existing := range s.rows {
		if existing.Device == device {
			delete(s.rows, id)
			removed++
		}
	}
	if removed == 0 {
		return 0, nil
	}
	return removed, s.save()
}

// load reads the subscriptions file once per run.
func (s *Store) load() error {
	if s.loaded {
		return nil
	}
	file, err := s.open(SubscriptionsFile, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		s.loaded = true
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, subscriptionsLimit+1))
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrUnreadable, SubscriptionsFile, err)
	}
	if len(data) > subscriptionsLimit {
		return fmt.Errorf("%w: %s is larger than %d bytes", ErrUnreadable, SubscriptionsFile, subscriptionsLimit)
	}
	var stored struct {
		Subscriptions []storedRow `json:"subscriptions"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		// A subscriptions list that will not parse is not an identity, and
		// there is nothing to lose by starting a new one: every browser that
		// is still subscribed will be back the next time the page loads,
		// because the page asks this machine what it holds.
		s.logf("push: %s will not parse, starting from an empty list: %v",
			filepath.Join(s.dir, SubscriptionsFile), err)
		s.loaded = true
		return nil
	}
	for _, row := range stored.Subscriptions {
		// Checked on the way in as well as on the way out. A row of the wrong
		// shape cannot be encrypted to, and finding that out at send time
		// means a log line an hour after whatever wrote it has been forgotten.
		p256dh, err := DecodeBase64URL(row.P256dh)
		if err != nil || len(p256dh) != SubscriberKeyBytes {
			continue
		}
		secret, err := DecodeBase64URL(row.Auth)
		if err != nil || len(secret) != AuthSecretBytes {
			continue
		}
		if row.ID == "" || row.Endpoint == "" {
			continue
		}
		device := row.Device
		if device == "" {
			device = "?"
		}
		s.rows[row.ID] = Subscription{
			ID:       row.ID,
			Endpoint: row.Endpoint,
			P256dh:   p256dh,
			Auth:     secret,
			Device:   device,
			Origin:   WebAppOrigin(row.Origin),
			Created:  time.Unix(int64(row.Created), 0),
		}
	}
	s.loaded = true
	return nil
}

type storedRow struct {
	ID       string  `json:"id"`
	Endpoint string  `json:"endpoint"`
	P256dh   string  `json:"p256dh"`
	Auth     string  `json:"auth"`
	Device   string  `json:"device"`
	Origin   string  `json:"origin,omitempty"`
	Created  float64 `json:"created"`
}

// sorted is every row, oldest first, as the caller must not depend on map order.
func (s *Store) sorted() []Subscription {
	out := make([]Subscription, 0, len(s.rows))
	for _, row := range s.rows {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Created.Equal(out[j].Created) {
			return out[i].ID < out[j].ID
		}
		return out[i].Created.Before(out[j].Created)
	})
	return out
}

func (s *Store) save() error {
	rows := make([]storedRow, 0, len(s.rows))
	for _, row := range s.sorted() {
		rows = append(rows, storedRow{
			ID:       row.ID,
			Endpoint: row.Endpoint,
			P256dh:   EncodeBase64URL(row.P256dh),
			Auth:     EncodeBase64URL(row.Auth),
			Device:   row.Device,
			Origin:   row.Origin,
			Created:  float64(row.Created.Unix()),
		})
	}
	// Without HTML escaping: an `&` in an endpoint's query is one byte on
	// disk, not six, which is what the row limit's arithmetic assumes.
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(struct {
		Version       int         `json:"version"`
		Subscriptions []storedRow `json:"subscriptions"`
	}{Version: storeVersion, Subscriptions: rows}); err != nil {
		return err
	}
	body := out.Bytes()
	if len(body) > subscriptionsLimit {
		s.refused++
		s.refusedAt = time.Now()
		return fmt.Errorf("%w: %d bytes, and %s is read up to %d", ErrSubscriptionsTooLarge, len(body), SubscriptionsFile, subscriptionsLimit)
	}
	return s.writeAtomically(SubscriptionsFile, body)
}

// readSecret reads one base64 line and requires it to decode to exactly want
// bytes. The middle result is whether the file is there at all, and it is true
// even when the contents are wrong — the caller needs to tell "no identity yet"
// from "an identity that will not parse", because only one of those may mint.
func (s *Store) readSecret(name string, want int) ([]byte, bool, error) {
	file, err := s.open(name, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, secretLimit+1))
	if err != nil {
		return nil, false, fmt.Errorf("%w: %s: %v", ErrUnreadable, name, err)
	}
	if len(data) > secretLimit {
		return nil, false, fmt.Errorf("%w: %s is larger than %d bytes", ErrUnreadable, name, secretLimit)
	}
	text := strings.TrimRight(string(data), "\n")
	raw, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return nil, true, fmt.Errorf("%w: %s: %v", ErrUnreadable, name, err)
	}
	if len(raw) != want {
		return nil, true, fmt.Errorf("%w: %s holds %d bytes, want %d", ErrUnreadable, name, len(raw), want)
	}
	return raw, true, nil
}

// open opens one of this directory's files as the file it is and only when it
// is a plain file. The Lstat before refuses a link that is there, O_NOFOLLOW
// refuses one that appears after it, and the same-file check after refuses a
// swap. It is cloudkeys's, unchanged.
func (s *Store) open(name string, flag int, perm os.FileMode) (*os.File, error) {
	path := filepath.Join(s.dir, name)
	for range 2 {
		before, err := os.Lstat(path)
		exists := err == nil
		switch {
		case exists && !before.Mode().IsRegular():
			return nil, fmt.Errorf("%w: %s", ErrNotRegular, path)
		case !exists && !(errors.Is(err, os.ErrNotExist) && flag&os.O_CREATE != 0):
			return nil, err
		}
		mode := flag &^ (os.O_CREATE | os.O_EXCL)
		if !exists {
			mode = flag | os.O_EXCL
		}
		file, err := os.OpenFile(path, mode|noFollow, perm)
		if !exists && errors.Is(err, os.ErrExist) {
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

func (s *Store) writeAtomically(name string, body []byte) (err error) {
	if existing, statErr := os.Lstat(filepath.Join(s.dir, name)); statErr == nil && !existing.Mode().IsRegular() {
		return fmt.Errorf("%w: %s", ErrNotRegular, filepath.Join(s.dir, name))
	}
	temporary, err := os.CreateTemp(s.dir, name+".*")
	if err != nil {
		return err
	}
	path := temporary.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	// Every time, not only at creation: an atomic write replaces the file, and
	// the replacement does not inherit the mode of what it replaced.
	if err = temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err = temporary.Write(body); err != nil {
		_ = temporary.Close()
		return err
	}
	if err = temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	if err = os.Rename(path, filepath.Join(s.dir, name)); err != nil {
		return err
	}
	syncDir(s.dir)
	return nil
}

func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

func within(path, dir string) bool {
	relative, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return relative == "." || !strings.HasPrefix(relative, "..")
}

func resolved(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return path
	}
	return filepath.Join(parent, filepath.Base(path))
}
