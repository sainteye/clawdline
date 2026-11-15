package projectassistantstore

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestOutputSnapshotsAreImmutableBoundedAndIntegrityChecked(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := Outputs{Dir: t.TempDir()}
	key := pa.BotKey{MachineID: "source", BotID: "bot"}
	data := bytes.Repeat([]byte{0, 1, 255, 3}, pa.MaxOutputChunkBytes/4+1)
	path := filepath.Join(root, "binary")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := s.Snapshot(ctx, key, root, path, false, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	out.ID = "output"
	out.Name = "binary"
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	reopened := Outputs{Dir: s.Dir}
	first, err := reopened.Chunk(ctx, key, out, 0)
	if err != nil || !bytes.Equal(first, data[:pa.MaxOutputChunkBytes]) {
		t.Fatal("first chunk", err)
	}
	last, err := reopened.Chunk(ctx, key, out, pa.MaxOutputChunkBytes)
	if err != nil || !bytes.Equal(last, data[pa.MaxOutputChunkBytes:]) {
		t.Fatal("last chunk", err)
	}
	if _, err = reopened.Chunk(ctx, pa.BotKey{MachineID: "source", BotID: "foreign"}, out, 0); pa.Code(err) != pa.NotFound {
		t.Fatal("foreign", err)
	}
	for _, offset := range []int64{-1, out.Bytes + 1} {
		if _, err = reopened.Chunk(ctx, key, out, offset); pa.Code(err) != pa.Invalid {
			t.Fatal("offset", err)
		}
	}
	if err = os.WriteFile(filepath.Join(s.botDir(key), out.SnapshotID), bytes.Repeat([]byte{'x'}, len(data)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Chunk(ctx, key, out, 0); pa.Code(err) != pa.Invalid {
		t.Fatal("corruption", err)
	}
	if err = os.Remove(filepath.Join(s.botDir(key), out.SnapshotID)); err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Chunk(ctx, key, out, 0); pa.Code(err) != pa.NotFound {
		t.Fatal("missing snapshot", err)
	}
}

func TestOutputSnapshotRefusesEscapesSpecialFilesAndInterruptedCopies(t *testing.T) {
	root := t.TempDir()
	s := Outputs{Dir: t.TempDir()}
	key := pa.BotKey{MachineID: "m", BotID: "b"}
	ctx := context.Background()
	for _, path := range []string{"../escape", root, "missing"} {
		if _, err := s.Snapshot(ctx, key, root, path, false, func() error { return nil }); err == nil {
			t.Fatal("unsafe path", path)
		}
	}
	path := filepath.Join(root, "source")
	if err := os.WriteFile(path, bytes.Repeat([]byte{'a'}, pa.MaxOutputChunkBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, filepath.Join(root, "link")); err == nil {
		if _, err = s.Snapshot(ctx, key, root, "link", false, func() error { return nil }); pa.Code(err) != pa.ScopeDenied {
			t.Fatal("symlink", err)
		}
	}
	calls := 0
	_, err := s.Snapshot(ctx, key, root, path, false, func() error {
		calls++
		if calls == 2 {
			return failure(pa.Revoked, "interrupted")
		}
		return nil
	})
	if pa.Code(err) != pa.Revoked {
		t.Fatal(err)
	}
	rows, err := os.ReadDir(s.botDir(key))
	if err != nil || len(rows) != 0 {
		t.Fatal("partial snapshot escaped", rows, err)
	}
	if err = os.WriteFile(path, bytes.Repeat([]byte{'a'}, pa.MaxArtifactBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Snapshot(ctx, key, root, path, true, func() error { return nil }); pa.Code(err) != pa.CapacityFull {
		t.Fatal("HTML bound", err)
	}
	if err = os.Truncate(path, pa.MaxOutputBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Snapshot(ctx, key, root, path, false, func() error { return nil }); pa.Code(err) != pa.CapacityFull {
		t.Fatal("file bound", err)
	}
}
