package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	botstore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	_ "modernc.org/sqlite"
)

// Recovery is an offline operation on an explicitly named new file. It never
// opens the daemon's database, resolves a provider or starts a restored turn.
func projectAssistantsArchive(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fail := func(err error) int {
		var failure *pa.Failure
		if !errors.As(err, &failure) {
			failure = &pa.Failure{Code: pa.Invalid, Field: "archive_io"}
		}
		_ = json.NewEncoder(stderr).Encode(map[string]any{"error": failure})
		return 1
	}
	usage := func() int {
		fmt.Fprint(stderr, cliCopy("misc", "project_assistants.archive_usage", "Usage: clawdline project-assistants archive restore|export --database FILE --machine ID --bot ID --project ID --canonical ID --kind repository|place [--input ARCHIVE]\n"))
		return 2
	}
	if len(args) == 0 || args[0] != "restore" && args[0] != "export" {
		return usage()
	}
	verb := args[0]
	f := flag.NewFlagSet("archive", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	var database, input string
	var key pa.BotKey
	var project pa.ProjectScope
	var kind string
	f.StringVar(&database, "database", "", "")
	f.StringVar(&input, "input", "", "")
	f.StringVar(&key.MachineID, "machine", "", "")
	f.StringVar(&key.BotID, "bot", "", "")
	f.StringVar(&project.ProjectID, "project", "", "")
	f.StringVar(&project.CanonicalID, "canonical", "", "")
	f.StringVar(&kind, "kind", "", "")
	if f.Parse(args[1:]) != nil || f.NArg() != 0 || database == "" || key.BotID == "" || (verb == "restore") != (input != "") {
		return usage()
	}
	project.MachineID, project.Kind = key.MachineID, pa.ProjectKind(kind)
	if err := pa.ValidateScope(project); err != nil {
		return fail(err)
	}
	var err error
	database, err = filepath.Abs(database)
	if err != nil {
		return fail(err)
	}
	var archive botstore.Archive
	if verb == "restore" {
		archive, err = readBotArchive(input)
		if err == nil {
			err = matchBotArchive(archive, key, project)
		}
		if err == nil {
			err = restoreBotArchive(ctx, archive, database)
		}
		if err != nil {
			return fail(err)
		}
		// The input hash is an integrity receipt, not an authentication claim.
		err = json.NewEncoder(stdout).Encode(map[string]any{"restored": true, "input_schema": archive.Data.SchemaVersion, "schema_version": pa.SchemaVersion, "input_sha256": archive.Manifest.SHA256, "providers_started": 0})
		if err != nil {
			return fail(&pa.Failure{Code: pa.OutcomeUnknown, Field: "archive_receipt"})
		}
	} else {
		archive, err = exportBotArchive(ctx, database, key, project)
		if err != nil {
			return fail(err)
		}
		err = json.NewEncoder(stdout).Encode(archive)
	}
	if err != nil {
		return fail(err)
	}
	return 0
}

func readBotArchive(path string) (botstore.Archive, error) {
	var archive botstore.Archive
	f, err := os.Open(path)
	if err != nil {
		return archive, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return archive, err
	}
	if !info.Mode().IsRegular() {
		return archive, &pa.Failure{Code: pa.Invalid, Field: "archive_file"}
	}
	if info.Size() > capacity.MaximumBotArchiveBytes {
		return archive, &pa.Failure{Code: pa.CapacityFull, Field: "archive_bytes"}
	}
	raw, err := io.ReadAll(io.LimitReader(f, capacity.MaximumBotArchiveBytes+1))
	if err != nil {
		return archive, err
	}
	if len(raw) > capacity.MaximumBotArchiveBytes {
		return archive, &pa.Failure{Code: pa.CapacityFull, Field: "archive_bytes"}
	}
	// Read the version envelope before the strict current-shape decoder, so a
	// future archive with new fields is Unsupported rather than damaged JSON.
	var header struct {
		Data struct {
			SchemaVersion int
		}
		Manifest botstore.Manifest
	}
	if json.Unmarshal(raw, &header) != nil {
		return archive, &pa.Failure{Code: pa.Invalid, Field: "archive_json"}
	}
	if header.Manifest.ContractVersion != pa.ContractVersion || !botstore.SupportsArchiveSchema(header.Data.SchemaVersion) || header.Manifest.SchemaVersion != header.Data.SchemaVersion {
		return archive, &pa.Failure{Code: pa.Unsupported, Field: "manifest_version"}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&archive) != nil {
		return archive, &pa.Failure{Code: pa.Invalid, Field: "archive_json"}
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return archive, &pa.Failure{Code: pa.Invalid, Field: "archive_json"}
	}
	return archive, nil
}

// Reader admission is frozen independently of the Store's default export
// generation. A future writer increment does not silently expand this list.
func matchBotArchive(archive botstore.Archive, key pa.BotKey, project pa.ProjectScope) error {
	if archive.Data.Bot.Key != key || archive.Data.Bot.Project != project {
		return &pa.Failure{Code: pa.ScopeDenied, Field: "archive_target"}
	}
	return nil
}

func restoreBotArchive(ctx context.Context, archive botstore.Archive, destination string) error {
	// Complete Store validation and restoration before creating any target or
	// stamping its schema. Failed imports leave no recoverable-looking file.
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err = (botstore.Installer{}).Install(ctx, db); err != nil {
		return err
	}
	if err = botstore.New(db).RestoreArchive(ctx, archive); err != nil {
		return err
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if _, err = os.Lstat(destination + suffix); !errors.Is(err, os.ErrNotExist) {
			if err == nil {
				return &pa.Failure{Code: pa.IdempotencyConflict, Field: "archive_destination"}
			}
			return err
		}
	}
	stage, err := os.MkdirTemp(filepath.Dir(destination), ".bot-recovery-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage) // Only this call's private staging directory.
	path := filepath.Join(stage, "recovered.sqlite3")
	if _, err = db.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		return err
	}
	if err = os.Chmod(path, 0600); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	// Link publishes an already complete database and never replaces a file,
	// including a destination created concurrently after the preflight above.
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Link(path, destination); errors.Is(err, os.ErrExist) {
		return &pa.Failure{Code: pa.IdempotencyConflict, Field: "archive_destination"}
	}
	if err != nil {
		return err
	}
	if syncBotArchiveDirectory(filepath.Dir(destination)) != nil {
		// Publication already happened. Preserve the complete target and make
		// uncertainty explicit; never remove it or retry an import automatically.
		return &pa.Failure{Code: pa.OutcomeUnknown, Field: "archive_publication"}
	}
	return nil
}

func exportBotArchive(ctx context.Context, database string, key pa.BotKey, project pa.ProjectScope) (botstore.Archive, error) {
	var archive botstore.Archive
	// A file URI treats '?' and '#' as path bytes, never as SQLite options.
	dsn := (&url.URL{Scheme: "file", Path: database, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return archive, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var version int
	if err = db.QueryRowContext(ctx, "SELECT version FROM project_assistant_schema WHERE id=1").Scan(&version); err != nil {
		return archive, &pa.Failure{Code: pa.Unsupported, Field: "schema_version"}
	}
	if version != pa.SchemaVersion {
		return archive, &pa.Failure{Code: pa.Unsupported, Field: "schema_version"}
	}
	archive, err = botstore.New(db).ExportArchive(ctx, key)
	if err == nil {
		err = matchBotArchive(archive, key, project)
	}
	return archive, err
}
