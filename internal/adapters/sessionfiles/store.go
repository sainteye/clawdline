// Package sessionfiles retains proven outputs of one conversation, independently
// of a terminal coordinate. It never discovers outputs by walking a workspace.
package sessionfiles

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/documents"
)

const MaximumFiles = 200
const MaximumIndexBytes = 2 * 1024 * 1024
const MaximumPathBytes = 4096
const MaximumSessions = 256
const MaximumStoreBytes = 64 * 1024 * 1024

var indexLock sync.Mutex
var ErrUnavailable = errors.New("output_index_unavailable")
var ErrFull = errors.New("output_index_full")
var ErrInvalid = errors.New("output_path_invalid")
var ErrMissing = errors.New("output_not_found")
var ErrChanged = errors.New("output_changed")
var ErrLarge = errors.New("output_too_large")
var ErrRange = errors.New("output_range_invalid")

type File struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	AbsolutePath string `json:"absolute_path"`
	Kind         string `json:"kind"`
	FirstAt      int64  `json:"first_at"`
	UpdatedAt    int64  `json:"updated_at"`
	Updates      int    `json:"updates"`
	Bytes        int64  `json:"bytes"`
	State        string `json:"state"`
	MediaType    string `json:"media_type"`
	Source       string `json:"source"`
	Anchor       int64  `json:"anchor"`
	// Root is chosen at publication, never supplied by a paired reader.
	ContentRevision string `json:"file_revision,omitempty"`
	Root            string `json:"root,omitempty"`
	Relative        string `json:"relative,omitempty"`
}
type pendingChange struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

type index struct {
	Version          int                        `json:"version"`
	Key              string                     `json:"key"`
	Record           string                     `json:"record"`
	Offset           int64                      `json:"offset"`
	Prefix           string                     `json:"prefix"`
	Files            []File                     `json:"files"`
	Full             bool                       `json:"full"`
	RecordIncomplete bool                       `json:"record_incomplete,omitempty"`
	Pending          map[string][]pendingChange `json:"pending,omitempty"`
}
type Listing struct {
	Files    []File `json:"files"`
	More     bool   `json:"more"`
	Partial  bool   `json:"partial"`
	Coverage string `json:"coverage"`
}
type Store struct{ Dir string }

func New(dir string) *Store             { return &Store{Dir: filepath.Join(dir, "session-outputs")} }
func digest(s string) string            { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func (s *Store) path(key string) string { return filepath.Join(s.Dir, digest(key)+".json") }
func (s *Store) load(key string) (index, error) {
	out := index{Version: 1, Key: key, Files: []File{}}
	f, err := os.Open(s.path(key))
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return out, ErrUnavailable
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaximumIndexBytes+1))
	if err != nil || len(b) > MaximumIndexBytes || json.Unmarshal(b, &out) != nil || out.Key != key || out.Version != 1 || len(out.Files) > MaximumFiles || len(out.Pending) > MaximumFiles {
		return index{}, ErrUnavailable
	}
	return out, nil
}
func (s *Store) save(out index) error {
	b, err := json.Marshal(out)
	if err != nil || len(b) > MaximumIndexBytes {
		return ErrFull
	}
	if os.MkdirAll(s.Dir, 0700) != nil {
		return ErrUnavailable
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return ErrUnavailable
	}
	total := int64(len(b))
	count := 1
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") && filepath.Join(s.Dir, entry.Name()) != s.path(out.Key) {
			i, e := entry.Info()
			if e != nil {
				return ErrUnavailable
			}
			total += i.Size()
			count++
		}
	}
	if total > MaximumStoreBytes || count > MaximumSessions {
		return ErrFull
	}
	f, err := os.CreateTemp(s.Dir, ".index-")
	if err != nil {
		return ErrUnavailable
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return ErrUnavailable
	}
	if os.Rename(name, s.path(out.Key)) != nil {
		return ErrUnavailable
	}
	return nil
}

// Register explicitly publishes an exact regular file. Files outside the
// workspace require this machine-authenticated operation, never a read query.
func (s *Store) Register(key, cwd, path, kind, source string, at, anchor int64) (File, error) {
	indexLock.Lock()
	defer indexLock.Unlock()
	out, err := s.load(key)
	if err != nil {
		return File{}, err
	}
	f, err := publishedFile(cwd, path, kind, source, at, anchor)
	if err != nil {
		return File{}, err
	}
	f, err = merge(&out, f)
	if err != nil {
		if errors.Is(err, ErrFull) {
			if e := s.save(out); e != nil {
				return File{}, e
			}
		}
		return File{}, err
	}
	if err = s.save(out); err != nil {
		return File{}, err
	}
	return f, nil
}
func publishedFile(cwd, path, kind, source string, at, anchor int64) (File, error) {
	if path == "" || len(path) > MaximumPathBytes || strings.ContainsAny(path, "\x00\r\n") {
		return File{}, ErrInvalid
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return File{}, ErrInvalid
	}
	path = filepath.Clean(path)
	// Publishing a symlink must not turn into permission to follow its future target.
	info, err := os.Lstat(path)
	missing := os.IsNotExist(err) && source == "native"
	if !missing && (err != nil || !info.Mode().IsRegular()) {
		return File{}, ErrInvalid
	}
	canonical, err := filepath.EvalSymlinks(path)
	if missing {
		parent, e := filepath.EvalSymlinks(filepath.Dir(path))
		if e != nil {
			return File{}, ErrInvalid
		}
		canonical = filepath.Join(parent, filepath.Base(path))
	} else if err != nil {
		return File{}, ErrInvalid
	}
	if len(canonical) > MaximumPathBytes {
		return File{}, ErrInvalid
	}
	root := filepath.Dir(canonical)
	rel := filepath.Base(canonical)
	display := path
	if r, e := filepath.Rel(cwd, path); e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		display = filepath.ToSlash(r)
	}
	if at <= 0 && source != "native" {
		at = time.Now().Unix()
	}
	if kind != "created" && kind != "modified" {
		kind = "published"
	}
	f := File{ID: digest(canonical), Name: filepath.Base(path), Path: display, AbsolutePath: canonical, Root: root, Relative: rel, Kind: kind, FirstAt: at, UpdatedAt: at, Updates: 1, Source: source, Anchor: anchor}
	if info != nil {
		f.ContentRevision = digest(info.ModTime().UTC().Format(time.RFC3339Nano) + "/" + intText(info.Size()))
	}
	inspect(&f)
	return f, nil
}
func merge(out *index, f File) (File, error) {
	for i, old := range out.Files {
		if old.ID == f.ID {
			// Re-reading the same native row is idempotent; repeated explicit publication
			// refreshes availability without inventing another edit.
			if old.FirstAt == 0 || f.FirstAt == 0 {
				f.FirstAt = 0
			} else {
				f.FirstAt = min(old.FirstAt, f.FirstAt)
			}
			f.Updates = old.Updates
			newNative := f.Source == "native" && f.Anchor > old.Anchor
			// A publication and the native receipt for the same current bytes
			// are two observations of one edit, not two edits.
			duplicatePublication := f.Source == "native" && old.Source != "native" && f.ContentRevision == old.ContentRevision && f.UpdatedAt <= old.UpdatedAt
			if (newNative && !duplicatePublication) || (f.Source != "native" && f.ContentRevision != old.ContentRevision) {
				f.Updates++
			}
			if f.UpdatedAt < old.UpdatedAt {
				f.UpdatedAt = old.UpdatedAt
			}
			if f.Anchor < old.Anchor {
				f.Anchor = old.Anchor
			}
			if f.Source != "native" && old.Source == "native" && f.ContentRevision == old.ContentRevision {
				f.Source = old.Source
			}
			if old.Kind == "created" {
				f.Kind = "created"
			}
			out.Files[i] = f
			return f, nil
		}
	}
	if len(out.Files) >= MaximumFiles {
		out.Full = true
		return File{}, ErrFull
	}
	out.Files = append(out.Files, f)
	return f, nil
}
func inspect(f *File) {
	f.State = "missing"
	f.Bytes = 0
	f.MediaType = "application/octet-stream"
	file, err := openFile(*f)
	if err != nil {
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return
	}
	f.Bytes = info.Size()
	f.State = "available"
	if f.Bytes > documents.MaximumVideoBytes {
		f.State = "too_large"
	}
	switch strings.ToLower(filepath.Ext(f.Name)) {
	case ".png":
		f.MediaType = "image/png"
	case ".jpg", ".jpeg":
		f.MediaType = "image/jpeg"
	case ".gif":
		f.MediaType = "image/gif"
	case ".webp":
		f.MediaType = "image/webp"
	case ".md", ".markdown":
		f.MediaType = "text/markdown"
	case ".txt", ".go", ".ts", ".tsx", ".js", ".jsx", ".json", ".csv", ".css", ".py", ".sh", ".yaml", ".yml", ".toml":
		f.MediaType = "text/plain"
	case ".html":
		f.MediaType = "text/html"
	case ".mp4":
		f.MediaType = "video/mp4"
	case ".pdf":
		f.MediaType = "application/pdf"
	}
}
func openFile(f File) (*os.File, error) {
	if !filepath.IsAbs(f.Root) || f.Relative != filepath.Base(f.Relative) || f.Relative == "." || f.Relative == ".." || filepath.Join(f.Root, f.Relative) != f.AbsolutePath || digest(f.AbsolutePath) != f.ID {
		return nil, ErrInvalid
	}
	root, err := os.OpenRoot(f.Root)
	if err != nil {
		return nil, ErrMissing
	}
	defer root.Close()
	info, err := root.Lstat(f.Relative)
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrMissing
	}
	file, err := root.Open(f.Relative)
	if err != nil {
		return nil, ErrMissing
	}
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(info, after) {
		file.Close()
		return nil, ErrChanged
	}
	return file, nil
}

// List advances a bounded native-record scan and commits it atomically. A lost
// checkpoint is a failed read, never a successful empty list.
func (s *Store) List(ctx context.Context, key, cwd, record, provider string) (Listing, error) {
	indexLock.Lock()
	defer indexLock.Unlock()
	out, err := s.load(key)
	if err != nil {
		return Listing{}, err
	}
	more, coverage, err := scan(ctx, &out, cwd, record, provider)
	if err != nil {
		return Listing{}, err
	}
	if err = s.save(out); err != nil {
		return Listing{}, err
	}
	rows := append([]File{}, out.Files...)
	for i := range rows {
		inspect(&rows[i])
		rows[i].ContentRevision = ""
		rows[i].Root = ""
		rows[i].Relative = ""
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].UpdatedAt == rows[j].UpdatedAt {
			return rows[i].Path < rows[j].Path
		}
		return rows[i].UpdatedAt > rows[j].UpdatedAt
	})
	if out.RecordIncomplete && coverage == "native_and_published" {
		coverage = "record_incomplete"
	}
	return Listing{Files: rows, More: more, Partial: out.Full, Coverage: coverage}, nil
}

type Chunk struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	MediaType  string `json:"media_type"`
	TotalBytes int64  `json:"total_bytes"`
	Offset     int64  `json:"offset"`
	Revision   string `json:"revision"`
	Data       []byte `json:"data"`
}

func (s *Store) Read(ctx context.Context, key, id string, offset int64, revision string) (Chunk, error) {
	indexLock.Lock()
	out, err := s.load(key)
	indexLock.Unlock()
	if err != nil {
		return Chunk{}, err
	}
	var found *File
	for _, f := range out.Files {
		if f.ID == id {
			found = &f
			break
		}
	}
	if found == nil {
		return Chunk{}, ErrMissing
	}
	f, err := openFile(*found)
	if err != nil {
		return Chunk{}, err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return Chunk{}, ErrMissing
	}
	if before.Size() > documents.MaximumVideoBytes {
		return Chunk{}, ErrLarge
	}
	version := digest(found.ID + before.ModTime().UTC().Format(time.RFC3339Nano) + "/" + intText(before.Size()))
	if revision != "" && revision != version {
		return Chunk{}, ErrChanged
	}
	if offset < 0 || offset > before.Size() || offset%documents.MaximumVideoChunkBytes != 0 || (offset > 0 && revision == "") {
		return Chunk{}, ErrRange
	}
	if err = ctx.Err(); err != nil {
		return Chunk{}, err
	}
	data := make([]byte, min(int64(documents.MaximumVideoChunkBytes), before.Size()-offset))
	if _, err = io.ReadFull(io.NewSectionReader(f, offset, int64(len(data))), data); err != nil {
		return Chunk{}, ErrChanged
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return Chunk{}, ErrChanged
	}
	inspect(found)
	return Chunk{ID: id, Name: found.Name, MediaType: found.MediaType, TotalBytes: before.Size(), Offset: offset, Revision: version, Data: data}, ctx.Err()
}

// Usage reports the retained store, never an invented occupancy of source files.
func (s *Store) Usage() (sessions int, bytes, largestIndex, maxFiles, maxPath int64, err error) {
	indexLock.Lock()
	defer indexLock.Unlock()
	entries, e := os.ReadDir(s.Dir)
	if os.IsNotExist(e) {
		return
	}
	if e != nil {
		err = ErrUnavailable
		return
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, e := entry.Info()
		if e != nil {
			err = ErrUnavailable
			return
		}
		sessions++
		bytes += info.Size()
		largestIndex = max(largestIndex, info.Size())
		if info.Size() > MaximumIndexBytes {
			err = ErrUnavailable
			return
		}
		raw, e := os.ReadFile(filepath.Join(s.Dir, entry.Name()))
		var out index
		if e != nil || json.Unmarshal(raw, &out) != nil {
			err = ErrUnavailable
			return
		}
		maxFiles = max(maxFiles, int64(len(out.Files)))
		for _, f := range out.Files {
			maxPath = max(maxPath, int64(len(f.AbsolutePath)))
		}
	}
	return
}
