package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	botstore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type botArchiveLostReceipt struct{}

func (botArchiveLostReceipt) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestBotArchiveLostReceiptPreservesCompleteTargetAndExportNeverMigrates(t *testing.T) {
	archive, raw := actualLegacyBotArchive(t)
	dir := t.TempDir()
	input, target := filepath.Join(dir, "input.json"), filepath.Join(dir, "recovered.db")
	if err := os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var errs bytes.Buffer
	args := append(botArchiveArgs("restore", target, archive), "--input", input)
	if code := projectAssistantsCommand(context.Background(), args, dir, botArchiveLostReceipt{}, &errs); code != 1 || !strings.Contains(errs.String(), `"code":"pa_outcome_unknown"`) || !strings.Contains(errs.String(), `"field":"archive_receipt"`) {
		t.Fatalf("lost success receipt: code=%d error=%s", code, &errs)
	}
	var out bytes.Buffer
	if code := projectAssistantsCommand(context.Background(), botArchiveArgs("export", target, archive), dir, &out, &errs); code != 0 {
		t.Fatalf("complete target unreadable: %d %s", code, &errs)
	}
	var restored botstore.Archive
	if err := json.Unmarshal(out.Bytes(), &restored); err != nil {
		t.Fatal(err)
	}
	archive.Data.SchemaVersion = restored.Data.SchemaVersion
	if !reflect.DeepEqual(archive.Data, restored.Data) {
		t.Fatal("lost receipt lost committed archive facts")
	}
	// A future on-disk schema is rejected by read-only export without an
	// Installer or a metadata downgrade, even though this command created it.
	db, err := sql.Open("sqlite", target)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("UPDATE project_assistant_schema SET version=99 WHERE id=1")
	if closeErr := db.Close(); err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errs.Reset()
	if code := projectAssistantsCommand(context.Background(), botArchiveArgs("export", target, archive), dir, &out, &errs); code != 1 || out.Len() != 0 || !strings.Contains(errs.String(), string(pa.Unsupported)) {
		t.Fatalf("future database: code=%d out=%s error=%s", code, &out, &errs)
	}
	after, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("read-only refusal changed future schema bytes")
	}
}

func actualLegacyBotArchive(t *testing.T) (botstore.Archive, []byte) {
	t.Helper()
	raw, err := os.ReadFile("../../internal/adapters/projectassistantstore/testdata/schema1-archive.json")
	if err != nil {
		t.Fatal(err)
	}
	var archive botstore.Archive
	if err = json.Unmarshal(raw, &archive); err != nil {
		t.Fatal(err)
	}
	return archive, raw
}

func currentBotArchive(t *testing.T) botstore.Archive {
	t.Helper()
	legacy, _ := actualLegacyBotArchive(t)
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	if err = (botstore.Installer{}).Install(ctx, db); err != nil {
		t.Fatal(err)
	}
	s := botstore.New(db)
	if err = s.RestoreArchive(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	b := legacy.Data.Bot
	revision := legacy.Data.Memory.Revision
	command := func(op pa.Operation, id string) pa.Command {
		sum := sha256.Sum256([]byte(id))
		return pa.Command{Key: b.Key, Operation: op, IdempotencyKey: id, ContentHash: fmt.Sprintf("%x", sum)}
	}
	for _, id := range []string{"forgotten-note", "live-note"} {
		entry := pa.MemoryEntry{ID: id, Key: b.Key, Project: b.Project, Kind: pa.TopicMemory, Title: "Synthetic note", Summary: "Synthetic summary", Text: "Synthetic body", Revision: revision, Sources: []pa.MemorySource{{ID: id, Key: b.Key, Project: b.Project, Kind: "message", Revision: revision}}}
		v, e := s.Save(ctx, app.MemoryWrite{Command: command(pa.MemorySaveOperation, id), ExpectedRevision: revision, Entry: entry})
		if e != nil {
			t.Fatal(e)
		}
		revision = v.Revision
	}
	if _, err = s.Forget(ctx, app.ForgetRequest{Command: command(pa.ForgetOperation, "forget-note"), Project: b.Project, ExpectedRevision: revision, SourceIDs: []string{"forgotten-note"}}); err != nil {
		t.Fatal(err)
	}
	archive, err := s.ExportArchive(ctx, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	return archive
}

func TestBotArchiveCLICurrentNotesTombstonesAndRefusalsLeaveNoTarget(t *testing.T) {
	valid := currentBotArchive(t)
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	input, destination := filepath.Join(dir, "current.json"), filepath.Join(dir, "current.sqlite3")
	if err = os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	args := append(botArchiveArgs("restore", destination, valid), "--input", input)
	if code := projectAssistantsCommand(context.Background(), args, dir, &out, &errs); code != 0 {
		t.Fatalf("current restore=%d %s", code, &errs)
	}
	out.Reset()
	if code := projectAssistantsCommand(context.Background(), botArchiveArgs("export", destination, valid), dir, &out, &errs); code != 0 {
		t.Fatalf("current export=%d %s", code, &errs)
	}
	var after botstore.Archive
	if err = json.Unmarshal(out.Bytes(), &after); err != nil || !reflect.DeepEqual(valid, after) {
		t.Fatal("current archive lost notes, tombstones, context or receipts", err)
	}
	for _, test := range []struct {
		name string
		edit func(*botstore.Archive)
		code pa.ErrorCode
	}{
		{"future", func(a *botstore.Archive) {
			a.Data.SchemaVersion = pa.SchemaVersion + 1
			a.Manifest.SchemaVersion = a.Data.SchemaVersion
		}, pa.Unsupported},
		{"version-mismatch", func(a *botstore.Archive) { a.Manifest.SchemaVersion = 11 }, pa.Unsupported},
		{"integrity", func(a *botstore.Archive) { a.Data.Bot.Name = "Damaged" }, pa.Invalid},
		{"foreign-project", func(a *botstore.Archive) { a.Data.Bot.Project.CanonicalID = "foreign" }, pa.ScopeDenied},
		{"foreign-bot", func(a *botstore.Archive) { a.Data.Bot.Key.BotID = "foreign" }, pa.ScopeDenied},
		{"late-fact", func(a *botstore.Archive) {
			a.Data.Events = append(a.Data.Events, a.Data.Events[0])
			b, _ := json.Marshal(a.Data)
			sum := sha256.Sum256(b)
			a.Manifest.SHA256 = fmt.Sprintf("%x", sum)
		}, pa.Invalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Detach all nested values so one case cannot change another input.
			var changed botstore.Archive
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			test.edit(&changed)
			b, _ := json.Marshal(changed)
			d := t.TempDir()
			in, target := filepath.Join(d, "input.json"), filepath.Join(d, "recovered.db")
			if err := os.WriteFile(in, b, 0600); err != nil {
				t.Fatal(err)
			}
			var out, errs bytes.Buffer
			code := projectAssistantsCommand(context.Background(), append(botArchiveArgs("restore", target, valid), "--input", in), d, &out, &errs)
			var failure struct {
				Error pa.Failure `json:"error"`
			}
			if json.Unmarshal(errs.Bytes(), &failure) != nil || code != 1 || failure.Error.Code != test.code || out.Len() != 0 {
				t.Fatalf("code=%d out=%s error=%s", code, &out, &errs)
			}
			if _, err := os.Lstat(target); !os.IsNotExist(err) {
				t.Fatal("failed import created target", err)
			}
			entries, _ := os.ReadDir(d)
			if len(entries) != 1 {
				t.Fatal("failed import left staging residue")
			}
		})
	}
}

func TestBotArchiveCLIRejectsOversizeTrailingFutureFieldsAndMissingExport(t *testing.T) {
	archive, raw := actualLegacyBotArchive(t)
	for _, name := range []string{"oversize", "trailing", "future-fields"} {
		t.Run(name, func(t *testing.T) {
			d := t.TempDir()
			input, target := filepath.Join(d, "input.json"), filepath.Join(d, "target.db")
			var data []byte
			want := pa.Invalid
			switch name {
			case "oversize":
				f, err := os.Create(input)
				if err != nil {
					t.Fatal(err)
				}
				if err = f.Truncate(capacity.MaximumBotArchiveBytes + 1); err != nil {
					t.Fatal(err)
				}
				f.Close()
				want = pa.CapacityFull
			case "trailing":
				data = append(append([]byte{}, raw...), []byte(" {}")...)
			case "future-fields":
				var fields map[string]any
				json.Unmarshal(raw, &fields)
				fields["data"].(map[string]any)["SchemaVersion"] = float64(pa.SchemaVersion + 1)
				fields["data"].(map[string]any)["future_field"] = true
				fields["manifest"].(map[string]any)["schema_version"] = float64(pa.SchemaVersion + 1)
				data, _ = json.Marshal(fields)
				want = pa.Unsupported
			}
			if data != nil {
				if err := os.WriteFile(input, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			var out, errs bytes.Buffer
			code := projectAssistantsCommand(context.Background(), append(botArchiveArgs("restore", target, archive), "--input", input), d, &out, &errs)
			if code != 1 || !strings.Contains(errs.String(), string(want)) {
				t.Fatalf("code=%d error=%s", code, &errs)
			}
			if _, err := os.Lstat(target); !os.IsNotExist(err) {
				t.Fatal("refusal created target")
			}
		})
	}
	d := t.TempDir()
	target := filepath.Join(d, "missing.db")
	var out, errs bytes.Buffer
	if code := projectAssistantsCommand(context.Background(), botArchiveArgs("export", target, archive), d, &out, &errs); code != 1 {
		t.Fatal("missing export succeeded")
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatal("read-only export created a database")
	}
}

func TestBotArchivePublicationHasOneWinnerAndKeepsExistingSidecars(t *testing.T) {
	archive, _ := actualLegacyBotArchive(t)
	target := filepath.Join(t.TempDir(), "recovered.db")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- restoreBotArchive(context.Background(), archive, target) }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if pa.Code(err) != pa.IdempotencyConflict {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("publication winners=%d", success)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		d := t.TempDir()
		target := filepath.Join(d, "recovered.db")
		sidecar := target + suffix
		if err := os.WriteFile(sidecar, []byte("existing bytes"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := restoreBotArchive(context.Background(), archive, target); pa.Code(err) != pa.IdempotencyConflict {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(sidecar)
		if string(b) != "existing bytes" {
			t.Fatal("existing sidecar changed")
		}
		if _, err := os.Lstat(target); !os.IsNotExist(err) {
			t.Fatal("sidecar refusal created a target")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	target = filepath.Join(t.TempDir(), "cancelled.db")
	if restoreBotArchive(ctx, archive, target) == nil {
		t.Fatal("cancelled restore succeeded")
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatal("cancelled restore left target")
	}
}

func botArchiveArgs(verb, database string, archive botstore.Archive) []string {
	p := archive.Data.Bot.Project
	return []string{"archive", verb, "--database", database, "--machine", p.MachineID, "--bot", archive.Data.Bot.Key.BotID, "--project", p.ProjectID, "--canonical", p.CanonicalID, "--kind", string(p.Kind)}
}

func TestBotArchiveCLIConsumesActualLegacyAndReexportsRestoredFacts(t *testing.T) {
	archive, raw := actualLegacyBotArchive(t)
	dir := t.TempDir()
	input, database := filepath.Join(dir, "input.json"), filepath.Join(dir, "restored.sqlite3")
	if err := os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	args := append(botArchiveArgs("restore", database, archive), "--input", input)
	if code := projectAssistantsCommand(context.Background(), args, dir, &out, &errs); code != 0 {
		t.Fatalf("actual legacy input: code=%d error=%s", code, &errs)
	}
	var receipt map[string]any
	if err := json.Unmarshal(out.Bytes(), &receipt); err != nil || receipt["restored"] != true {
		t.Fatalf("receipt=%s err=%v", &out, err)
	}
	out.Reset()
	if code := projectAssistantsCommand(context.Background(), botArchiveArgs("export", database, archive), dir, &out, &errs); code != 0 {
		t.Fatalf("export: code=%d error=%s", code, &errs)
	}
	var restored botstore.Archive
	if err := json.Unmarshal(out.Bytes(), &restored); err != nil {
		t.Fatal(err)
	}
	archive.Data.SchemaVersion = restored.Data.SchemaVersion
	if !reflect.DeepEqual(archive.Data, restored.Data) {
		t.Fatal("restored history, memory, context or receipts changed")
	}
	before, err := os.ReadFile(database)
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := projectAssistantsCommand(context.Background(), args, dir, &out, &errs); code == 0 {
		t.Fatal("existing recovery target was replaced")
	}
	after, err := os.ReadFile(database)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("refusal changed existing recovery bytes")
	}
}
