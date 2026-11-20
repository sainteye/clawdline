package projectassistantstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func inputTestFile(id, name string, data []byte) pa.InputFile {
	sum := sha256.Sum256(data)
	return pa.InputFile{ID: id, Name: name, Bytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}
func inputTestFence() pa.Fence {
	return pa.Fence{Key: pa.BotKey{MachineID: "source", BotID: "bot"}, Project: pa.ProjectScope{MachineID: "source", ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}, ConversationID: "conversation", BindingID: "binding", Generation: 1}
}
func inputCheck(context.Context) error { return nil }

func TestInputsChunksRetryIntegrityAndOwnership(t *testing.T) {
	ctx := context.Background()
	s := Inputs{Dir: t.TempDir()}
	fence := inputTestFence()
	data := bytes.Repeat([]byte{0, 255, 10, 3}, pa.MaxInputChunkBytes/4+1)
	file := inputTestFile("11111111-1111-4111-8111-111111111111", "binary", data)
	put := func(offset int64, b []byte, done bool) error {
		_, err := s.Put(ctx, fence, file, offset, b, done, inputCheck)
		return err
	}
	if err := put(1, data[:1], false); pa.Code(err) != pa.Invalid {
		t.Fatal("out-of-order first chunk", err)
	}
	if err := put(0, data[:pa.MaxInputChunkBytes], false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Resolve(ctx, fence, file.ID); pa.Code(err) != pa.Invalid {
		t.Fatal("unfinished upload resolved", err)
	}
	if err := put(0, data[:pa.MaxInputChunkBytes], false); err != nil {
		t.Fatal("identical retry", err)
	}
	if err := put(0, []byte{123}, false); pa.Code(err) != pa.IdempotencyConflict {
		t.Fatal("conflicting retry", err)
	}
	if err := put(int64(pa.MaxInputChunkBytes), data[pa.MaxInputChunkBytes:], true); err != nil {
		t.Fatal(err)
	}
	if err := put(int64(pa.MaxInputChunkBytes), data[pa.MaxInputChunkBytes:], true); err != nil {
		t.Fatal("final retry", err)
	}
	actual, path, err := s.Resolve(ctx, fence, file.ID)
	if err != nil || actual != file || !filepath.IsAbs(path) {
		t.Fatal(actual, path, err)
	}
	var assembled []byte
	for offset := int64(0); offset < file.Bytes; {
		b, e := s.Chunk(ctx, fence, file, offset)
		if e != nil {
			t.Fatal(e)
		}
		assembled = append(assembled, b...)
		offset += int64(len(b))
	}
	if !bytes.Equal(assembled, data) {
		t.Fatal("binary bytes changed")
	}
	for _, change := range []func(*pa.Fence){func(f *pa.Fence) { f.Key.BotID = "foreign" }, func(f *pa.Fence) { f.Project.ProjectID = "foreign" }, func(f *pa.Fence) { f.ConversationID = "foreign" }, func(f *pa.Fence) { f.Generation++ }} {
		foreign := fence
		change(&foreign)
		if _, _, e := s.Resolve(ctx, foreign, file.ID); e == nil {
			t.Fatal("foreign upload resolved")
		}
	}
	newEpoch := fence
	newEpoch.Generation++
	if _, err = s.SentPath(ctx, newEpoch, file); err != nil {
		t.Fatal("durable sent metadata did not survive a new execution", err)
	}
	if err = os.WriteFile(path, bytes.Repeat([]byte{9}, len(data)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Chunk(ctx, fence, file, 0); pa.Code(err) != pa.Invalid {
		t.Fatal("corruption served", err)
	}
}

func TestInputsEmptyLimitsCancellationAndSafePaths(t *testing.T) {
	ctx := context.Background()
	s := Inputs{Dir: t.TempDir()}
	fence := inputTestFence()
	file := inputTestFile("11111111-1111-4111-8111-111111111111", "empty", nil)
	if _, err := s.Put(ctx, fence, file, 0, nil, true, inputCheck); err != nil {
		t.Fatal(err)
	}
	if b, err := s.Chunk(ctx, fence, file, 0); err != nil || len(b) != 0 {
		t.Fatal("empty file", err)
	}
	for _, name := range []string{"../outside", "/absolute", "a/b", "a\\b", ".", "..", "control\nname"} {
		bad := file
		bad.Name = name
		if _, err := s.Put(ctx, fence, bad, 0, nil, true, inputCheck); err == nil {
			t.Fatal("path accepted", name)
		}
	}
	bad := file
	bad.Bytes = pa.MaxInputBytes + 1
	if _, err := s.Put(ctx, fence, bad, 0, nil, true, inputCheck); err == nil {
		t.Fatal("oversize accepted")
	}
	if _, err := s.Put(ctx, fence, file, 0, make([]byte, pa.MaxInputChunkBytes+1), true, inputCheck); err == nil {
		t.Fatal("oversize chunk accepted")
	}
	if _, err := s.Put(ctx, fence, file, 0, nil, true, func(context.Context) error { return &pa.Failure{Code: pa.Revoked} }); pa.Code(err) != pa.Revoked {
		t.Fatal("revocation ignored", err)
	}
	// An input directory cannot turn into a symlink to another owned input.
	_, path, err := s.Resolve(ctx, fence, file.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err = os.WriteFile(outside, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, path); err != nil {
		t.Skip("symlink not available", err)
	}
	if _, _, err = s.Resolve(ctx, fence, file.ID); err == nil {
		t.Fatal("symlink resolved")
	}
	if _, err = s.Put(ctx, fence, file, 0, nil, true, inputCheck); err == nil {
		t.Fatal("symlink overwritten")
	}
	if err = (Attachments{Dir: s.Dir}).RemoveBot(fence.Key); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(s.botDir(fence.Key)); !os.IsNotExist(err) {
		t.Fatal("delete retained private files", err)
	}
}

func TestInputsRetainHistoryWithoutRefusingNewUploads(t *testing.T) {
	ctx := context.Background()
	s := Inputs{Dir: t.TempDir()}
	fence := inputTestFence()
	// Previous versions refused this Bot after 128 upload directories.
	for i := 0; i < 128; i++ {
		file := inputTestFile(fmt.Sprintf("%08x-1111-4111-8111-111111111111", i), "empty", nil)
		if _, err := s.Put(ctx, fence, file, 0, nil, true, inputCheck); err != nil {
			t.Fatal(i, err)
		}
	}
	file := inputTestFile("ffffffff-1111-4111-8111-111111111111", "empty", nil)
	if _, err := s.Put(ctx, fence, file, 0, nil, true, inputCheck); err != nil {
		t.Fatal("retained uploads blocked a new file", err)
	}
	first := inputTestFile("00000000-1111-4111-8111-111111111111", "empty", nil)
	foreign := fence
	foreign.Project.ProjectID = "other-project"
	if err := s.Remove(ctx, foreign, first); pa.Code(err) != pa.ScopeDenied {
		t.Fatal("foreign takeback removed a file", err)
	}
	if err := s.Remove(ctx, fence, first); err != nil {
		t.Fatal("takeback did not remove its file", err)
	}
	if err := s.Remove(ctx, fence, first); err != nil {
		t.Fatal("takeback retry", err)
	}
	if _, err := s.Put(ctx, fence, file, 0, nil, true, inputCheck); err != nil {
		t.Fatal("replaying an existing upload after takeback", err)
	}
}

func TestInputsAcceptTheExactFileByteLimit(t *testing.T) {
	ctx := context.Background()
	s := Inputs{Dir: t.TempDir()}
	fence := inputTestFence()
	block := make([]byte, pa.MaxInputChunkBytes)
	hash := sha256.New()
	for offset := 0; offset < pa.MaxInputBytes; offset += len(block) {
		hash.Write(block[:min(len(block), pa.MaxInputBytes-offset)])
	}
	file := pa.InputFile{ID: "11111111-1111-4111-8111-111111111111", Name: "limit", Bytes: pa.MaxInputBytes, SHA256: hex.EncodeToString(hash.Sum(nil))}
	for offset := int64(0); offset < file.Bytes; {
		data := block[:min(int64(len(block)), file.Bytes-offset)]
		done := offset+int64(len(data)) == file.Bytes
		next, err := s.Put(ctx, fence, file, offset, data, done, inputCheck)
		if err != nil {
			t.Fatal(offset, err)
		}
		offset = next
	}
	_, path, err := s.Resolve(ctx, fence, file.ID)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != file.Bytes {
		t.Fatal(info, err)
	}
	if _, err = s.Chunk(ctx, fence, file, file.Bytes+1); err == nil {
		t.Fatal("past-end offset accepted")
	}
	if b, err := s.Chunk(ctx, fence, file, file.Bytes); err != nil || len(b) != 0 {
		t.Fatal("EOF", err)
	}
}

func TestInputsTakebackRetriesWithoutDeletingUnknownBytes(t *testing.T) {
	ctx := context.Background()
	s := Inputs{Dir: t.TempDir()}
	fence := inputTestFence()
	file := inputTestFile("11111111-1111-4111-8111-111111111111", "owned", []byte("owned"))
	if _, err := s.Put(ctx, fence, file, 0, []byte("owned"), true, inputCheck); err != nil {
		t.Fatal(err)
	}
	_, path, err := s.Resolve(ctx, fence, file.ID)
	if err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(filepath.Dir(path), "unknown")
	if err = os.WriteFile(unknown, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.Remove(ctx, fence, file); err == nil {
		t.Fatal("unknown contents were recursively removed")
	}
	if data, e := os.ReadFile(unknown); e != nil || string(data) != "preserve" {
		t.Fatal("unknown bytes changed", e)
	}
	if _, err = os.Stat(filepath.Join(s.botDir(fence.Key), file.ID+".json")); err != nil {
		t.Fatal("retry metadata lost", err)
	}
	if err = os.Remove(unknown); err != nil {
		t.Fatal(err)
	}
	if err = s.Remove(ctx, fence, file); err != nil {
		t.Fatal("partial cleanup did not converge", err)
	}
	if _, _, err = s.Resolve(ctx, fence, file.ID); pa.Code(err) != pa.NotFound {
		t.Fatal("cleanup left upload metadata", err)
	}
}
