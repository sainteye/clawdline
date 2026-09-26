package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/work"
)

func imageFixture(t *testing.T) (*Store, string, context.Context) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dir, context.Background()
}

func addItemImage(t *testing.T, s *Store, itemID, imageID, mediaType string, data []byte) {
	t.Helper()
	at := time.Unix(1_790_000_000, 0)
	err := s.WriteWorkV2(context.Background(), func(tx *WorkV2Tx) error {
		if _, err := tx.Item(itemID); err != nil {
			if err := tx.CreateItem(v2Item(itemID, at), "local", `{}`); err != nil {
				return err
			}
		}
		return tx.AddImage(work.ImageV2{ID: imageID, WorkID: itemID, Title: "ref", MediaType: mediaType,
			Width: 1, Height: 1, CreatedBy: "local", CreatedAt: at}, data)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func hexSum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// A PNG and a JPEG each land in their own file, named by id and stored type,
// and the database holds neither's bytes.
func TestReferenceImagesAreFilesNamedByIDAndType(t *testing.T) {
	s, dir, ctx := imageFixture(t)
	item := "40000000-0000-4000-8000-000000000001"
	png, jpg := "40000000-0000-4000-8000-000000000002", "40000000-0000-4000-8000-000000000003"
	pngBytes, jpgBytes := []byte("\x89PNG screenshot"), []byte("\xFF\xD8\xFF photograph")
	addItemImage(t, s, item, png, "image/png", pngBytes)
	addItemImage(t, s, item, jpg, "image/jpeg", jpgBytes)
	for _, c := range []struct {
		id, name, mediaType string
		data                []byte
	}{{png, png + ".png", "image/png", pngBytes}, {jpg, jpg + ".jpg", "image/jpeg", jpgBytes}} {
		onDisk, err := os.ReadFile(filepath.Join(ReferenceImagesDir(dir), c.name))
		if err != nil || !bytes.Equal(onDisk, c.data) {
			t.Fatalf("%s on disk: %q %v", c.name, onDisk, err)
		}
		got, mediaType, ok, err := s.WorkV2ImageBytes(ctx, c.id)
		if err != nil || !ok || mediaType != c.mediaType || !bytes.Equal(got, c.data) {
			t.Fatalf("%s read: %q %q %v %v", c.id, got, mediaType, ok, err)
		}
		var sum string
		if err := s.db.QueryRow(`SELECT sha256 FROM work_v2_images WHERE id=?`, c.id).Scan(&sum); err != nil || sum != hexSum(c.data) {
			t.Fatalf("%s row hash %q, %v", c.id, sum, err)
		}
	}
	if has, err := hasColumn(s.db, "work_v2_images", "data"); err != nil || has {
		t.Fatalf("a fresh store has a data column: %v %v", has, err)
	}
}

// A file that is gone, shorter, longer or other bytes of the same length is a
// named refusal, counted, and never an empty picture.
func TestAMissingOrDifferentFileIsANamedRefusal(t *testing.T) {
	s, dir, ctx := imageFixture(t)
	item, id := "41000000-0000-4000-8000-000000000001", "41000000-0000-4000-8000-000000000002"
	data := []byte("\x89PNG the stored picture")
	addItemImage(t, s, item, id, "image/png", data)
	path := filepath.Join(ReferenceImagesDir(dir), id+".png")
	for _, c := range []struct {
		name  string
		bytes []byte
		want  error
	}{
		{"other bytes, same length", bytes.Repeat([]byte{'x'}, len(data)), ErrReferenceImageMismatch},
		{"shorter", data[:4], ErrReferenceImageMismatch},
		{"longer", append(append([]byte{}, data...), 'x'), ErrReferenceImageMismatch},
		{"gone", nil, ErrReferenceImageMissing},
	} {
		if c.bytes == nil {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(path, c.bytes, 0o600); err != nil {
			t.Fatal(err)
		}
		got, _, ok, err := s.WorkV2ImageBytes(ctx, id)
		if !errors.Is(err, c.want) || !ok || got != nil {
			t.Fatalf("%s: %q ok=%v err=%v, want %v", c.name, got, ok, err, c.want)
		}
	}
	if st := s.ReferenceImageFileStats(); st.Refused != 4 {
		t.Fatalf("refusals counted: %+v", st)
	}
	// An id nobody stored is still simply not there.
	if _, _, ok, err := s.WorkV2ImageBytes(ctx, "41000000-0000-4000-8000-000000000009"); ok || err != nil {
		t.Fatalf("unknown id: %v %v", ok, err)
	}
}

func addTodoWithImages(t *testing.T, s *Store, todoID string, ids ...string) {
	t.Helper()
	at := time.Unix(1_790_000_000, 0)
	err := s.WriteWorkV2(context.Background(), func(tx *WorkV2Tx) error {
		if err := tx.CreateDirectTodo(work.DirectTodoV2{ID: todoID, SessionID: "session-a", Text: "look",
			CreatedBy: "local", CreatedAt: at, Version: 1}); err != nil {
			return err
		}
		for n, id := range ids {
			mediaType := "image/png"
			if n%2 == 1 {
				mediaType = "image/jpeg"
			}
			if err := tx.AddDirectTodoImage(work.DirectTodoImageV2{ID: id, TodoID: todoID, Title: id,
				MediaType: mediaType, Width: 1, Height: 1, Position: int64(n), CreatedBy: "local", CreatedAt: at},
				[]byte("picture "+id)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A to-do typed into a Session carries every picture from its file, and one
// missing file refuses the set rather than send the to-do without it.
func TestATodoIsTypedWithItsPicturesFromFiles(t *testing.T) {
	s, dir, ctx := imageFixture(t)
	a, b := "42000000-0000-4000-8000-000000000001", "42000000-0000-4000-8000-000000000002"
	addTodoWithImages(t, s, "todo-files", a, b)
	payloads, err := s.DirectTodoV2ImagePayloads(ctx, "todo-files")
	if err != nil || len(payloads) != 2 || string(payloads[0].Data) != "picture "+a ||
		payloads[1].Image.MediaType != "image/jpeg" || string(payloads[1].Data) != "picture "+b {
		t.Fatalf("payloads: %+v %v", payloads, err)
	}
	if err := os.Remove(filepath.Join(ReferenceImagesDir(dir), b+".jpg")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DirectTodoV2ImagePayloads(ctx, "todo-files"); !errors.Is(err, ErrReferenceImageMissing) {
		t.Fatalf("a missing picture answered %v", err)
	}
}

// Deleting an image, or the to-do its rows cascade from, removes the files
// once the delete commits; a delete that does not commit keeps them.
func TestDeletesTakeTheirFilesAfterTheCommit(t *testing.T) {
	s, dir, ctx := imageFixture(t)
	files := ReferenceImagesDir(dir)
	item, img := "43000000-0000-4000-8000-000000000001", "43000000-0000-4000-8000-000000000002"
	addItemImage(t, s, item, img, "image/png", []byte("item picture"))
	a, b := "43000000-0000-4000-8000-000000000003", "43000000-0000-4000-8000-000000000004"
	addTodoWithImages(t, s, "todo-gone", a, b)

	rollback := errors.New("changed my mind")
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		if err := tx.DeleteImage(img); err != nil {
			return err
		}
		if _, err := tx.DeleteDirectTodo("todo-gone", "session-a"); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	for _, name := range []string{img + ".png", a + ".png", b + ".jpg"} {
		if _, err := os.Stat(filepath.Join(files, name)); err != nil {
			t.Fatalf("a delete that rolled back took %s: %v", name, err)
		}
	}

	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error { return tx.DeleteImage(img) }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(files, img+".png")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a deleted image kept its file: %v", err)
	}
	// Another Session's delete of the to-do deletes nothing and keeps the files.
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		deleted, err := tx.DeleteDirectTodo("todo-gone", "session-b")
		if deleted {
			t.Fatal("another Session deleted the to-do")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		_, err := tx.DeleteDirectTodo("todo-gone", "session-a")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{a + ".png", b + ".jpg"} {
		if _, err := os.Stat(filepath.Join(files, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the cascade left %s: %v", name, err)
		}
	}
}

// A picture whose transaction does not commit leaves no file behind.
func TestAnAddThatDoesNotCommitLeavesNoFile(t *testing.T) {
	s, dir, ctx := imageFixture(t)
	item := "44000000-0000-4000-8000-000000000001"
	addItemImage(t, s, item, "44000000-0000-4000-8000-000000000002", "image/png", []byte("kept"))
	id := "44000000-0000-4000-8000-000000000003"
	later := errors.New("the item write after it failed")
	err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		if err := tx.AddImage(work.ImageV2{ID: id, WorkID: item, Title: "t", MediaType: "image/jpeg",
			Width: 1, Height: 1, CreatedBy: "local", CreatedAt: time.Unix(1, 0)}, []byte("\xFF\xD8\xFF")); err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(ReferenceImagesDir(dir), id+".jpg")); err != nil {
			t.Fatalf("the file was not written before the row: %v", err)
		}
		return later
	})
	if !errors.Is(err, later) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ReferenceImagesDir(dir), id+".jpg")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a rolled-back picture left its file: %v", err)
	}
	// A refused row (its item is not there) leaves none either.
	err = s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		return tx.AddImage(work.ImageV2{ID: id, WorkID: "no-such-item", Title: "t", MediaType: "image/png",
			Width: 1, Height: 1, CreatedBy: "local", CreatedAt: time.Unix(1, 0)}, []byte("x"))
	})
	if err == nil {
		t.Fatal("an image for no item was stored")
	}
	if _, err := os.Stat(filepath.Join(ReferenceImagesDir(dir), id+".png")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused picture left its file: %v", err)
	}
}

// The sweep removes old files no row names and old temporaries, and leaves a
// named file, a young orphan and every name it did not write.
func TestTheSweepRemovesOnlyItsOwnOldUnnamedFiles(t *testing.T) {
	s, dir, ctx := imageFixture(t)
	files := ReferenceImagesDir(dir)
	named := "45000000-0000-4000-8000-000000000002"
	addItemImage(t, s, "45000000-0000-4000-8000-000000000001", named, "image/png", []byte("named"))
	old := time.Now().Add(-2 * ReferenceImageOrphanAgeLimit)
	write := func(name string, at time.Time) {
		path := filepath.Join(files, name)
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	orphan := "45000000-0000-4000-8000-00000000000a.jpg"
	wrongType := named + ".jpg" // the row names the .png
	temp := "45000000-0000-4000-8000-00000000000b.png.tmp-45000000-0000-4000-8000-00000000000c"
	young := "45000000-0000-4000-8000-00000000000d.png"
	foreign := []string{"notes.txt", "45000000-0000-4000-8000-00000000000e.gif", "IMG_0001.PNG",
		"45000000-0000-4000-8000-00000000000F.png", ".DS_Store"}
	write(orphan, old)
	write(wrongType, old)
	write(temp, old)
	write(young, time.Now())
	for _, f := range foreign {
		write(f, old)
	}
	if err := os.Chtimes(filepath.Join(files, named+".png"), old, old); err != nil {
		t.Fatal(err)
	}
	removed, err := s.SweepReferenceImages(ctx, time.Now())
	if err != nil || removed != 3 {
		t.Fatalf("removed %d, %v", removed, err)
	}
	for _, gone := range []string{orphan, wrongType, temp} {
		if _, err := os.Stat(filepath.Join(files, gone)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s survived: %v", gone, err)
		}
	}
	for _, kept := range append([]string{named + ".png", young}, foreign...) {
		if _, err := os.Stat(filepath.Join(files, kept)); err != nil {
			t.Fatalf("%s was removed: %v", kept, err)
		}
	}
	if st := s.ReferenceImageFileStats(); st.Removed != 3 || st.LastSweep.IsZero() {
		t.Fatalf("stats: %+v", st)
	}
	if again, err := s.SweepReferenceImages(ctx, time.Now()); err != nil || again != 0 {
		t.Fatalf("second sweep: %d %v", again, err)
	}
}

// legacyImageStore writes a store as it was before the pictures moved: both
// tables with a `data` column, n pictures each, alternately PNG and JPEG.
func legacyImageStore(t *testing.T, n int) (string, map[string][]byte) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	at := time.Unix(1_790_000_000, 0)
	item := "46000000-0000-4000-8000-000000000001"
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		if err := tx.CreateItem(v2Item(item, at), "local", `{}`); err != nil {
			return err
		}
		return tx.CreateDirectTodo(work.DirectTodoV2{ID: "todo-legacy", SessionID: "session-a", Text: "t",
			CreatedBy: "local", CreatedAt: at, Version: 1})
	}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	db, err := sql.Open("sqlite", filepath.Join(dir, DBFile)+"?_foreign_keys=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	blobs := map[string][]byte{}
	for _, tbl := range []struct{ table, owner, ownerID string }{
		{"work_v2_images", "work_id", item}, {"session_direct_todo_images", "todo_id", "todo-legacy"},
	} {
		if _, err := db.Exec(`DROP TABLE ` + tbl.table); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`CREATE TABLE ` + tbl.table + ` (
  id TEXT PRIMARY KEY, ` + tbl.owner + ` TEXT NOT NULL REFERENCES ` + map[string]string{"work_id": "work_v2_items", "todo_id": "session_direct_todos"}[tbl.owner] + `(id) ON DELETE CASCADE,
  title TEXT NOT NULL, media_type TEXT NOT NULL CHECK (media_type IN ('image/png','image/jpeg')), data BLOB NOT NULL,
  byte_count INTEGER NOT NULL, width INTEGER NOT NULL, height INTEGER NOT NULL, position INTEGER NOT NULL,
  created_by TEXT NOT NULL, created_at INTEGER NOT NULL)`); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < n; i++ {
			prefix := "47"
			if tbl.table == "session_direct_todo_images" {
				prefix = "48"
			}
			id := fmt.Sprintf("%s000000-0000-4000-8000-%012d", prefix, i)
			mediaType, data := "image/png", bytes.Repeat([]byte{byte(i)}, 1000+i)
			if i%2 == 1 {
				mediaType = "image/jpeg"
				data = append([]byte{0xFF, 0xD8, 0xFF}, data...)
			}
			blobs[id] = data
			if _, err := db.Exec(`INSERT INTO `+tbl.table+` VALUES (?,?,?,?,?,?,1,1,?,'local',1)`,
				id, tbl.ownerID, "legacy", mediaType, data, len(data), i); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir, blobs
}

// The move out of the database is resumable: a failure after a step's files
// are written and before its rows are cleared loses nothing, the next Open
// carries on, and an Open after that changes nothing.
func TestTheMoveToFilesResumesAfterAFailureAndIsIdempotent(t *testing.T) {
	dir, blobs := legacyImageStore(t, 5)
	defer func(n int64) { imageMoveBatchBytes = n }(imageMoveBatchBytes)
	imageMoveBatchBytes = 2500 // two pictures a step
	crash := errors.New("injected: the machine went away")
	defer func() { imageMoveFault = nil }()
	imageMoveFault = func(table string, step int) error {
		if table == "session_direct_todo_images" && step == 1 {
			return crash
		}
		return nil
	}
	if _, err := Open(dir); !errors.Is(err, crash) {
		t.Fatalf("the first Open answered %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, DBFile))
	if err != nil {
		t.Fatal(err)
	}
	var moved, carrying int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM work_v2_images WHERE sha256<>'' AND length(data)=0) +
      (SELECT COUNT(*) FROM session_direct_todo_images WHERE sha256<>'' AND length(data)=0),
      (SELECT COUNT(*) FROM session_direct_todo_images WHERE sha256='' AND length(data)>0)`).Scan(&moved, &carrying); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if moved != 5+2 || carrying != 3 {
		t.Fatalf("after the failure: %d moved, %d still in the database", moved, carrying)
	}

	imageMoveFault = nil
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for id, want := range blobs {
		got, _, ok, err := s.WorkV2ImageBytes(ctx, id)
		if err != nil || !ok || !bytes.Equal(got, want) {
			t.Fatalf("%s after the move: %d bytes, %v %v", id, len(got), ok, err)
		}
		var sum string
		if err := s.db.QueryRow(`SELECT sha256 FROM work_v2_images WHERE id=? UNION ALL
        SELECT sha256 FROM session_direct_todo_images WHERE id=?`, id, id).Scan(&sum); err != nil || sum != hexSum(want) {
			t.Fatalf("%s hash %q %v", id, sum, err)
		}
	}
	for _, table := range []string{"work_v2_images", "session_direct_todo_images"} {
		if has, err := hasColumn(s.db, table, "data"); err != nil || has {
			t.Fatalf("%s still has its data column: %v %v", table, has, err)
		}
	}
	var integrity string
	if err := s.db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity_check: %q %v", integrity, err)
	}
	rows, err := s.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Fatal("foreign_key_check found a row")
	}
	rows.Close()
	// The cascade still reaches the rebuilt table.
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		_, err := tx.DeleteDirectTodo("todo-legacy", "session-a")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n := countFiles(t, ReferenceImagesDir(dir)); n != 5 {
		t.Fatalf("after deleting the to-do, %d files", n)
	}
	s.Close()

	third, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer third.Close()
	if n := countFiles(t, ReferenceImagesDir(dir)); n != 5 {
		t.Fatalf("a third Open changed the files: %d", n)
	}
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if !strings.Contains(e.Name(), ".tmp-") {
			n++
		}
	}
	return n
}

// A row whose id cannot name a file stops the move before any bytes are
// cleared, and says which row.
func TestAnIDThatCannotNameAFileStopsTheMove(t *testing.T) {
	dir, _ := legacyImageStore(t, 1)
	db, err := sql.Open("sqlite", filepath.Join(dir, DBFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE work_v2_images SET id='../escape'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "../escape") {
		t.Fatalf("Open answered %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.png")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a file was written outside the directory: %v", err)
	}
}
