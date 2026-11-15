package projectassistantstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Outputs shares only the private storage root with input pictures. The output
// namespace and message metadata remain separate. Snapshot never holds SQL locks.
type Outputs struct{ Dir string }

// Path is for the owned native list tool, never the HTTP wire. A model must
// edit a copy: mutating the snapshot makes authenticated reads refuse it.
func (s Outputs) Path(key pa.BotKey, out pa.Output) (string, error) {
	if err := pa.ValidateOutput(out); err != nil {
		return "", err
	}
	name := out.SnapshotID
	if name == "" {
		name = out.SHA256
	}
	return filepath.Abs(filepath.Join(s.botDir(key), name))
}

func (s Outputs) botDir(key pa.BotKey) string {
	return filepath.Join((Attachments{Dir: s.Dir}).botDir(key), "outputs")
}

func (s Outputs) Snapshot(ctx context.Context, key pa.BotKey, directory, path string, artifact bool, check func() error) (pa.Output, error) {
	var out pa.Output
	if s.Dir == "" || check == nil {
		return out, failure(pa.Disabled, "output_store")
	}
	if filepath.IsAbs(path) {
		var err error
		path, err = filepath.Rel(directory, path)
		if err != nil {
			return out, failure(pa.ScopeDenied, "output_path")
		}
	}
	path = filepath.Clean(path)
	if !filepath.IsLocal(path) {
		return out, failure(pa.ScopeDenied, "output_path")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return out, failure(pa.NotFound, "output_root")
	}
	defer root.Close()
	// Inspect every component, then open under the kernel-enforced root. The
	// final descriptor must still name the regular file inspected here.
	parts := strings.Split(path, string(filepath.Separator))
	current := ""
	var before os.FileInfo
	for _, part := range parts {
		current = filepath.Join(current, part)
		before, err = root.Lstat(current)
		if err != nil {
			return out, failure(pa.NotFound, "output_path")
		}
		if before.Mode()&os.ModeSymlink != 0 {
			return out, failure(pa.ScopeDenied, "output_symlink")
		}
	}
	if !before.Mode().IsRegular() {
		return out, failure(pa.Invalid, "output_regular_file")
	}
	f, err := openOutput(root, path)
	if err != nil {
		return out, failure(pa.Invalid, "output_open")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(before, info) {
		return out, failure(pa.ScopeDenied, "output_file_changed")
	}
	limit := int64(pa.MaxOutputBytes)
	if artifact {
		limit = pa.MaxArtifactBytes
	}
	if info.Size() > limit {
		return out, failure(pa.CapacityFull, "output_bytes")
	}
	dir := s.botDir(key)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return out, err
	}
	tmp, err := os.CreateTemp(dir, ".unpublished-")
	if err != nil {
		return out, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	h := sha256.New()
	buffer := make([]byte, pa.MaxOutputChunkBytes)
	var size int64
	for {
		if err = ctx.Err(); err != nil {
			return out, failure(pa.Revoked, "output_cancelled")
		}
		if err = check(); err != nil {
			return out, err
		}
		n, e := f.Read(buffer)
		if n > 0 {
			size += int64(n)
			if size > limit {
				return out, failure(pa.CapacityFull, "output_bytes")
			}
			if _, err = io.MultiWriter(tmp, h).Write(buffer[:n]); err != nil {
				return out, err
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return out, failure(pa.Invalid, "output_copy")
		}
	}
	after, e := f.Stat()
	if e != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) || size != info.Size() {
		return out, failure(pa.StaleVersion, "output_source_changed")
	}
	if err = tmp.Sync(); err != nil {
		return out, err
	}
	if artifact {
		if _, err = tmp.Seek(0, io.SeekStart); err != nil {
			return out, err
		}
		data, e := io.ReadAll(io.LimitReader(tmp, pa.MaxArtifactBytes+1))
		if e != nil || !utf8.Valid(data) {
			return out, failure(pa.Invalid, "artifact_html")
		}
	}
	if err = tmp.Close(); err != nil {
		return out, err
	}
	if err = check(); err != nil {
		return out, err
	}
	out.Bytes = size
	out.SHA256 = hex.EncodeToString(h.Sum(nil))
	out.SnapshotID = "snapshot-" + strings.TrimPrefix(filepath.Base(tmp.Name()), ".unpublished-")
	if err = os.Rename(tmp.Name(), filepath.Join(dir, out.SnapshotID)); err != nil {
		return out, err
	}
	if err = syncOutputDirectory(dir); err != nil {
		_ = s.Discard(key, out)
		return out, err
	}
	return out, nil
}

// Chunk hashes the entire snapshot on every read before returning any bytes.
// The wire reply is bounded even for the largest permitted output.
func (s Outputs) Chunk(ctx context.Context, key pa.BotKey, out pa.Output, offset int64) ([]byte, error) {
	if err := pa.ValidateOutput(out); err != nil {
		return nil, err
	}
	if offset < 0 || offset > out.Bytes {
		return nil, failure(pa.Invalid, "output_offset")
	}
	root, err := os.OpenRoot(s.botDir(key))
	if err != nil {
		return nil, failure(pa.NotFound, "output_bytes")
	}
	defer root.Close()
	name := out.SnapshotID
	if name == "" {
		name = out.SHA256
	}
	f, err := openOutput(root, name)
	if err != nil {
		return nil, failure(pa.NotFound, "output_bytes")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != out.Bytes {
		return nil, failure(pa.Invalid, "output_integrity")
	}
	h := sha256.New()
	buffer := make([]byte, pa.MaxOutputChunkBytes)
	data := make([]byte, min(int64(pa.MaxOutputChunkBytes), out.Bytes-offset))
	var read int64
	for {
		if ctx.Err() != nil {
			return nil, failure(pa.Revoked, "output_cancelled")
		}
		n, e := f.Read(buffer)
		start := read
		read += int64(n)
		if read > out.Bytes {
			return nil, failure(pa.Invalid, "output_integrity")
		}
		h.Write(buffer[:n])
		left, right := max(start, offset), min(read, offset+int64(len(data)))
		if right > left {
			copy(data[left-offset:right-offset], buffer[left-start:right-start])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, failure(pa.Invalid, "output_integrity")
		}
	}
	if read != out.Bytes || hex.EncodeToString(h.Sum(nil)) != out.SHA256 {
		return nil, failure(pa.Invalid, "output_integrity")
	}
	return data, nil
}

// Discard only removes this publication's unique, unpublished snapshot. It
// cannot remove a digest shared by any committed card or a concurrent copy.
func (s Outputs) Discard(key pa.BotKey, out pa.Output) error {
	if !pa.OutputName(out.SnapshotID) || !strings.HasPrefix(out.SnapshotID, "snapshot-") {
		return failure(pa.Invalid, "output_snapshot")
	}
	return os.Remove(filepath.Join(s.botDir(key), out.SnapshotID))
}
