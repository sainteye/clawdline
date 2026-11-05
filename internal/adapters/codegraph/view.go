package codegraph

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type ViewNode struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	File      string `json:"file"`
	Location  string `json:"location"`
	Community string `json:"community"`
}
type ViewLink struct {
	Source     string `json:"source"`
	Target     string `json:"target"`
	Relation   string `json:"relation"`
	Confidence string `json:"confidence"`
}
type ViewData struct {
	UnverifiedSources int64       `json:"unverified_sources"`
	Build             BuildStatus `json:"build"`
	CanBuild          bool        `json:"can_build"`
	State             string      `json:"state"`
	Reason            string      `json:"reason"`
	CheckedAt         int64       `json:"checked_at"`
	TotalNodes        int64       `json:"total_nodes"`
	TotalLinks        int64       `json:"total_links"`
	Truncated         bool        `json:"truncated"`
	Nodes             []ViewNode  `json:"nodes"`
	Links             []ViewLink  `json:"links"`
	Watching          bool        `json:"watching"`
	LastSyncedAt      int64       `json:"last_synced_at"`
	Generation        string      `json:"generation"`
}

const (
	ViewNodeLimit             = 300
	ViewLinkLimit             = 900
	ViewTextLimit             = 120
	ViewPollMillisecondsLimit = 5000
)

func openStore(ctx context.Context, directory string) (*sql.DB, error) {
	var total int64
	for _, name := range []string{"codegraph.db", "codegraph.db-wal", "codegraph.db-shm"} {
		st, err := os.Lstat(filepath.Join(directory, name))
		if os.IsNotExist(err) && name != "codegraph.db" {
			continue
		}
		if err != nil || !st.Mode().IsRegular() {
			return nil, BuildError("index_unverified")
		}
		total += st.Size()
	}
	if total > StoreBytesLimit {
		return nil, BuildError("graph_limit")
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(directory, "codegraph.db"))}
	q := u.Query()
	q.Set("mode", "ro")
	q.Add("_pragma", "query_only(1)")
	q.Add("_pragma", "trusted_schema(0)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, BuildError("index_unverified")
	}
	db.SetMaxOpenConns(BuildWorkersLimit)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, BuildError("index_unverified")
	}
	return db, nil
}
func validateStore(ctx context.Context, directory string) error {
	db, err := openStore(ctx, directory)
	if err != nil {
		return err
	}
	defer db.Close()
	var state, version string
	if db.QueryRowContext(ctx, `SELECT value FROM project_metadata WHERE key='index_state'`).Scan(&state) != nil || state != "complete" {
		return BuildError("index_unverified")
	}
	if db.QueryRowContext(ctx, `SELECT value FROM project_metadata WHERE key='indexed_with_version'`).Scan(&version) != nil || version != Version {
		return BuildError("index_unverified")
	}
	var count int64
	if db.QueryRowContext(ctx, `SELECT count(*) FROM nodes`).Scan(&count) != nil || count == 0 {
		return BuildError("no_sources")
	}
	for _, query := range []string{`SELECT id,name,file_path,start_line FROM nodes LIMIT 0`, `SELECT source,target,kind,provenance FROM edges LIMIT 0`, `SELECT path,content_hash FROM files LIMIT 0`} {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return BuildError("index_unverified")
		}
		rows.Close()
	}
	return nil
}
func (b *Builder) View(ctx context.Context, root string) (ViewData, error) {
	resolved, err := canonical(root)
	if err != nil {
		return ViewData{}, err
	}
	var live serviceStatus
	var serviceErr error
	if b.native {
		if _, _, err := readManifest(resolved); err == nil {
			s, err := b.provisionService(resolved)
			serviceErr = err
			if err == nil {
				select {
				case <-s.initialized:
					s.mu.Lock()
					serviceErr = s.startErr
					s.mu.Unlock()
					if serviceErr == nil {
						serviceErr = s.call(ctx, "status", "", &live)
					}
				default:
					serviceErr = BuildError("busy")
				}
			}
		}
	}
	view, err := View(ctx, resolved)
	if err != nil {
		return view, err
	}
	view.Build = b.Status(resolved)
	view.CanBuild = (view.State == "missing" || view.State == "stale" || serviceErr != nil) && view.Build.State != "running"
	view.Watching = live.Watching
	view.LastSyncedAt = live.LastSyncedAt
	if serviceErr != nil {
		view.State = "stale"
		view.Reason = "codegraph_service_unavailable"
		if serviceErr == BuildError("busy") {
			view.Reason = "codegraph_reconciling"
		}
	}
	if live.State == "stale" {
		view.State = "stale"
		view.Reason = live.Reason
	}
	return view, nil
}

// View projects a bounded SQLite snapshot. No vendor HTML or graph attributes
// are sent to the browser; the native database is never rewritten for display.
func View(ctx context.Context, root string) (ViewData, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(CheckSecondsLimit)*time.Second)
	defer cancel()
	root, err := canonical(root)
	if err != nil {
		return ViewData{}, err
	}
	status, err := Inspect(ctx, root)
	out := ViewData{State: status.State, Reason: status.Reason, CheckedAt: time.Now().Unix(), Generation: status.Generation, Nodes: []ViewNode{}, Links: []ViewLink{}}
	if err != nil {
		return out, err
	}
	if status.State != Matched && status.State != "stale" {
		return out, nil
	}
	m, raw, err := readManifest(root)
	if err != nil {
		out.State = "unverified"
		return out, nil
	}
	directory := filepath.Join(root, ".codegraph")
	if err = validateStore(ctx, directory); err != nil {
		out.State = "unverified"
		out.Reason = "codegraph_index_unverified"
		return out, nil
	}
	db, err := openStore(ctx, directory)
	if err != nil {
		return out, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM nodes`).Scan(&out.TotalNodes); err != nil {
		return out, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM edges`).Scan(&out.TotalLinks); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT file_path,count(*) FROM nodes GROUP BY file_path`)
	if err != nil {
		return out, err
	}
	groups := 0
	for rows.Next() {
		var file string
		var count int64
		if err = rows.Scan(&file, &count); err != nil {
			rows.Close()
			return out, err
		}
		groups++
		if groups > MaxScanEntries {
			rows.Close()
			return out, BuildError("graph_limit")
		}
		if _, ok := m.Sources[file]; !ok {
			out.UnverifiedSources += count
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()
	rows, err = tx.QueryContext(ctx, `WITH degrees AS (SELECT source id,count(*) n FROM edges GROUP BY source UNION ALL SELECT target id,count(*) n FROM edges GROUP BY target), scores AS (SELECT id,sum(n) n FROM degrees GROUP BY id) SELECT n.id,n.name,n.file_path,n.start_line FROM nodes n LEFT JOIN scores s ON s.id=n.id ORDER BY coalesce(s.n,0) DESC,n.id LIMIT ?`, ViewNodeLimit)
	if err != nil {
		return out, err
	}
	ids := map[string]string{}
	nativeIDs := []any{}
	for rows.Next() {
		var id, label, file string
		var line int64
		if err = rows.Scan(&id, &label, &file, &line); err != nil {
			rows.Close()
			return out, err
		}
		mapped := strconv.Itoa(len(out.Nodes))
		ids[id] = mapped
		nativeIDs = append(nativeIDs, id)
		location, community := "", ""
		if _, ok := m.Sources[file]; ok && file != "" {
			location = fmt.Sprintf("L%d", line)
			community = path.Dir(file)
		} else {
			file = ""
		}
		out.Nodes = append(out.Nodes, ViewNode{ID: mapped, Label: viewText(label), File: viewText(file), Location: viewText(location), Community: viewText(community)})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()
	if len(nativeIDs) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(nativeIDs)), ",")
		arguments := append(append([]any{}, nativeIDs...), nativeIDs...)
		arguments = append(arguments, ViewLinkLimit)
		rows, err = tx.QueryContext(ctx, `SELECT source,target,kind,coalesce(provenance,'') FROM edges WHERE source IN (`+placeholders+`) AND target IN (`+placeholders+`) ORDER BY id LIMIT ?`, arguments...)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var a, z, kind, provenance string
			if err = rows.Scan(&a, &z, &kind, &provenance); err != nil {
				rows.Close()
				return out, err
			}
			confidence := "INFERRED"
			if kind == "contains" && provenance != "heuristic" {
				confidence = "EXTRACTED"
			}
			out.Links = append(out.Links, ViewLink{Source: ids[a], Target: ids[z], Relation: viewText(kind), Confidence: confidence})
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return out, err
		}
		rows.Close()
	}
	after, checkErr := Inspect(ctx, root)
	if checkErr != nil || after.State != Matched || after.Generation != status.Generation {
		out.State = "stale"
		out.Reason = "codegraph_source_changed"
	}
	out.Truncated = int64(len(out.Nodes)) < out.TotalNodes || int64(len(out.Links)) < out.TotalLinks
	again, err := os.ReadFile(filepath.Join(root, ownerFile))
	if err != nil || string(again) != string(raw) {
		out.State = "stale"
		out.Reason = "codegraph_index_changed"
	}
	return out, nil
}
func viewText(s string) string {
	r := []rune(s)
	if len(r) > ViewTextLimit {
		return string(r[:ViewTextLimit-1]) + "…"
	}
	return s
}
