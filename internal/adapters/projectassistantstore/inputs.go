package projectassistantstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Inputs owns a bounded private namespace. The aggregate is the only binding
// authority: a file becomes sent with the same transaction as its message.
// No SQL or Bot actor lock is held while uploading or hashing file bytes.
type Inputs struct{ Dir string }

// A single writer serializes bounded disk chunks, without a retained lock map.
var inputWriter sync.Mutex

type inputRecord struct {
	Owner app.InputOwner `json:"owner"`
	File  pa.InputFile   `json:"file"`
	Fence pa.Fence       `json:"fence"`
	Done  bool           `json:"done"`
}

func (s Inputs) botDir(key pa.BotKey) string {
	return filepath.Join((Attachments{Dir: s.Dir}).botDir(key), "inputs")
}

// Inspect every component on every platform; the Unix descriptor walk also
// refuses symlink races. The final handle must still be the inspected file.
func openInputRead(root *os.Root, path string) (*os.File, error) {
	if !filepath.IsLocal(path) {
		return nil, os.ErrPermission
	}
	var before os.FileInfo
	var err error
	partPath := ""
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		partPath = filepath.Join(partPath, part)
		before, err = root.Lstat(partPath)
		if err != nil {
			return nil, err
		}
		if before.Mode()&os.ModeSymlink != 0 {
			return nil, os.ErrPermission
		}
	}
	if !before.Mode().IsRegular() {
		return nil, os.ErrPermission
	}
	f, err := openOutput(root, path)
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		f.Close()
		return nil, os.ErrPermission
	}
	return f, nil
}

func (s Inputs) record(root *os.Root, id string) (inputRecord, error) {
	var rec inputRecord
	if !pa.ValidUploadID(id) {
		return rec, failure(pa.Invalid, "upload_id")
	}
	f, err := openInputRead(root, id+".json")
	if err != nil {
		return rec, failure(pa.NotFound, "upload")
	}
	defer f.Close()
	if err = json.NewDecoder(io.LimitReader(f, pa.MaxMessageBytes)).Decode(&rec); err != nil {
		return rec, failure(pa.Invalid, "upload_metadata")
	}
	if rec.File.ID != id || pa.ValidateInput(rec.File) != nil {
		return rec, failure(pa.Invalid, "upload_metadata")
	}
	return rec, nil
}

func (s Inputs) save(root *os.Root, rec inputRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	// The writer owns this fixed temporary name; a crash leaves no new slot.
	f, err := root.OpenFile(".record", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if os.IsExist(err) {
		_ = root.Remove(".record")
		f, err = root.OpenFile(".record", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	}
	if err != nil {
		return err
	}
	defer root.Remove(".record")
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err == nil {
		err = root.Rename(".record", rec.File.ID+".json")
	}
	return err
}

func (s Inputs) Put(ctx context.Context, fence pa.Fence, file pa.InputFile, offset int64, data []byte, done bool, check func(context.Context) error, owners ...app.InputOwner) (int64, error) {
	if s.Dir == "" || check == nil {
		return 0, failure(pa.Disabled, "input_store")
	}
	if err := pa.ValidateInput(file); err != nil {
		return 0, err
	}
	if offset < 0 || offset > file.Bytes || len(data) > pa.MaxInputChunkBytes || int64(len(data)) > file.Bytes-offset || len(data) == 0 && !done || done && offset+int64(len(data)) != file.Bytes {
		return 0, failure(pa.Invalid, "upload_chunk")
	}
	var owner app.InputOwner
	if len(owners) > 0 {
		owner = owners[0]
	}
	inputWriter.Lock()
	defer inputWriter.Unlock()
	if err := check(ctx); err != nil {
		return 0, err
	}
	dir := s.botDir(fence.Key)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return 0, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	rec, err := s.record(root, file.ID)
	if pa.Code(err) == pa.NotFound {
		if offset != 0 {
			return 0, failure(pa.Invalid, "upload_offset")
		}
		entries, e := os.ReadDir(dir)
		if e != nil {
			return 0, e
		}
		n := 0
		for _, entry := range entries {
			if entry.IsDir() {
				n++
			}
		}
		if n >= pa.MaxInputsPerBot {
			return 0, failure(pa.CapacityFull, "input_slots")
		}
		// Exclusive creation also refuses stale directories and symlinks.
		if e = root.Mkdir(file.ID, 0700); e != nil {
			return 0, failure(pa.Invalid, "upload_directory")
		}
		rec = inputRecord{File: file, Fence: fence, Owner: owner}
		if e = s.save(root, rec); e != nil {
			_ = root.Remove(file.ID)
			return 0, e
		}
	} else if err != nil {
		return 0, err
	}
	if rec.Owner != owner {
		return 0, failure(pa.StaleVersion, "upload_authority_or_configuration")
	}
	if rec.File != file || rec.Fence != fence {
		return 0, failure(pa.ScopeDenied, "upload_owner")
	}
	info, err := root.Lstat(file.ID)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return 0, failure(pa.Invalid, "upload_directory")
	}
	sub, err := root.OpenRoot(file.ID)
	if err != nil {
		return 0, err
	}
	defer sub.Close()
	f, err := openInputWrite(sub, file.Name)
	if err != nil {
		return 0, failure(pa.Invalid, "upload_file")
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > file.Bytes || offset > info.Size() {
		return 0, failure(pa.Invalid, "upload_offset")
	}
	if offset < info.Size() || rec.Done {
		if offset+int64(len(data)) > info.Size() {
			return 0, failure(pa.Invalid, "upload_offset")
		}
		old := make([]byte, len(data))
		n, e := f.ReadAt(old, offset)
		if e != nil && e != io.EOF || n != len(data) || !bytes.Equal(old, data) {
			return 0, failure(pa.IdempotencyConflict, "upload_chunk")
		}
	} else {
		if _, err = f.WriteAt(data, offset); err != nil {
			return 0, err
		}
		if err = f.Sync(); err != nil {
			return 0, err
		}
	}
	info, err = f.Stat()
	if err != nil {
		return 0, err
	}
	if done {
		if info.Size() != file.Bytes {
			return 0, failure(pa.Invalid, "upload_incomplete")
		}
		if err = verifyInput(ctx, f, file); err != nil {
			return 0, err
		}
		if err = check(ctx); err != nil {
			return 0, err
		}
		if err = syncOutputDirectory(filepath.Join(dir, file.ID)); err != nil {
			return 0, err
		}
		rec.Done = true
		if err = s.save(root, rec); err != nil {
			return 0, err
		}
		if err = syncOutputDirectory(dir); err != nil {
			return 0, err
		}
	}
	return offset + int64(len(data)), check(ctx)
}

func verifyInput(ctx context.Context, f *os.File, file pa.InputFile) error {
	_, err := verifiedInputChunk(ctx, f, file, file.Bytes)
	return err
}

func verifiedInputChunk(ctx context.Context, f *os.File, file pa.InputFile, offset int64) ([]byte, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != file.Bytes {
		return nil, failure(pa.Invalid, "upload_integrity")
	}
	h := sha256.New()
	buffer := make([]byte, pa.MaxInputChunkBytes)
	data := make([]byte, min(int64(pa.MaxInputChunkBytes), file.Bytes-offset))
	var size int64
	for {
		if ctx.Err() != nil {
			return nil, failure(pa.Revoked, "upload_cancelled")
		}
		n, e := f.Read(buffer)
		start := size
		size += int64(n)
		if size > file.Bytes {
			return nil, failure(pa.Invalid, "upload_integrity")
		}
		h.Write(buffer[:n])
		left, right := max(start, offset), min(size, offset+int64(len(data)))
		if right > left {
			copy(data[left-offset:right-offset], buffer[left-start:right-start])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
	}
	if size != file.Bytes || hex.EncodeToString(h.Sum(nil)) != file.SHA256 {
		return nil, failure(pa.Invalid, "upload_integrity")
	}
	return data, nil
}

// Resolve verifies the complete private file before admission or native use.
func (s Inputs) Resolve(ctx context.Context, fence pa.Fence, id string, owners ...app.InputOwner) (pa.InputFile, string, error) {
	if len(owners) > 0 {
		root, err := os.OpenRoot(s.botDir(fence.Key))
		if err != nil {
			return pa.InputFile{}, "", failure(pa.NotFound, "upload")
		}
		rec, err := s.record(root, id)
		root.Close()
		if err != nil {
			return pa.InputFile{}, "", err
		}
		if rec.Owner != owners[0] {
			return pa.InputFile{}, "", failure(pa.StaleVersion, "upload_authority_or_configuration")
		}
	}
	return s.resolve(ctx, fence, id, true)
}

func (s Inputs) resolve(ctx context.Context, fence pa.Fence, id string, staged bool) (pa.InputFile, string, error) {
	root, err := os.OpenRoot(s.botDir(fence.Key))
	if err != nil {
		return pa.InputFile{}, "", failure(pa.NotFound, "upload")
	}
	defer root.Close()
	rec, err := s.record(root, id)
	if err != nil {
		return rec.File, "", err
	}
	if staged && rec.Fence != fence || rec.Fence.Key != fence.Key || rec.Fence.Project != fence.Project || rec.Fence.ConversationID != fence.ConversationID {
		return rec.File, "", failure(pa.ScopeDenied, "upload_owner")
	}
	if !rec.Done {
		return rec.File, "", failure(pa.Invalid, "upload_incomplete")
	}
	f, err := openInputRead(root, filepath.Join(id, rec.File.Name))
	if err != nil {
		return rec.File, "", failure(pa.NotFound, "upload_file")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != rec.File.Bytes {
		return rec.File, "", failure(pa.Invalid, "upload_integrity")
	}
	if err = verifyInput(ctx, f, rec.File); err != nil {
		return rec.File, "", err
	}
	path, err := filepath.Abs(filepath.Join(s.botDir(fence.Key), id, rec.File.Name))
	return rec.File, path, err
}

// Chunk accepts durable metadata, not a URL or a client-supplied path.
func (s Inputs) Chunk(ctx context.Context, fence pa.Fence, file pa.InputFile, offset int64) ([]byte, error) {
	if err := pa.ValidateInput(file); err != nil {
		return nil, err
	}
	if offset < 0 || offset > file.Bytes {
		return nil, failure(pa.Invalid, "upload_offset")
	}
	root, err := os.OpenRoot(s.botDir(fence.Key))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	rec, err := s.record(root, file.ID)
	if err != nil {
		return nil, err
	}
	if !rec.Done || rec.File != file || rec.Fence.Key != fence.Key || rec.Fence.Project != fence.Project || rec.Fence.ConversationID != fence.ConversationID {
		return nil, failure(pa.ScopeDenied, "upload_owner")
	}
	f, err := openInputRead(root, filepath.Join(file.ID, file.Name))
	if err != nil {
		return nil, failure(pa.NotFound, "upload_file")
	}
	defer f.Close()
	return verifiedInputChunk(ctx, f, file, offset)
}

// SentPath is called only with a message already read from the aggregate.
// Its original upload epoch may precede the first native binding or a restart.
func (s Inputs) SentPath(ctx context.Context, fence pa.Fence, file pa.InputFile) (string, error) {
	actual, path, err := s.resolve(ctx, fence, file.ID, false)
	if err == nil && actual != file {
		return "", failure(pa.Invalid, "upload_integrity")
	}
	return path, err
}

// Remove accepts durable message metadata, never a caller-supplied path. It
// reclaims the private bytes and upload slot after a proven queued takeback.
// Missing bytes after a partial cleanup are safe to retry; unknown directory
// contents are preserved rather than recursively removed.
func (s Inputs) Remove(ctx context.Context, fence pa.Fence, file pa.InputFile) error {
	if err := pa.ValidateInput(file); err != nil {
		return err
	}
	inputWriter.Lock()
	defer inputWriter.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := os.OpenRoot(s.botDir(fence.Key))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	rec, err := s.record(root, file.ID)
	if err != nil {
		_, recordErr := root.Lstat(file.ID + ".json")
		_, dirErr := root.Lstat(file.ID)
		if os.IsNotExist(recordErr) && os.IsNotExist(dirErr) {
			return nil
		}
		return err
	}
	if rec.File != file || rec.Fence.Key != fence.Key || rec.Fence.Project != fence.Project || rec.Fence.ConversationID != fence.ConversationID {
		return failure(pa.ScopeDenied, "input_takeback_owner")
	}
	before, err := root.Lstat(file.ID)
	if err == nil {
		if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			return failure(pa.Invalid, "input_takeback_directory")
		}
		sub, e := root.OpenRoot(file.ID)
		if e != nil {
			return e
		}
		after, e := sub.Stat(".")
		if e != nil || !os.SameFile(before, after) {
			sub.Close()
			return failure(pa.Invalid, "input_takeback_directory")
		}
		e = sub.Remove(file.Name)
		sub.Close()
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		current, e := root.Lstat(file.ID)
		if e != nil || !os.SameFile(before, current) {
			return failure(pa.Invalid, "input_takeback_directory")
		}
		if e = root.Remove(file.ID); e != nil {
			return e
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = root.Remove(file.ID + ".json"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncOutputDirectory(s.botDir(fence.Key))
}
