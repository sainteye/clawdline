package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func paTargetsURL(machine, cursor string, size int) string {
	return "/v1/project-assistant-targets?" + url.Values{"machine_id": {machine}, "cursor": {cursor}, "page_size": {fmt.Sprint(size)}}.Encode()
}

func paTargetsRead(t *testing.T, f *paServerFixture, token, cursor string, size int) (contract.ProjectAssistantProjectsResponse, *httptest.ResponseRecorder) {
	t.Helper()
	w := f.request(t, "GET", paTargetsURL(f.key.MachineID, cursor, size), token, nil)
	paServerOK(t, w)
	var page contract.ProjectAssistantProjectsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if w.Header().Get("Cache-Control") != "no-store" || page.ContractVersion != pa.ContractVersion {
		t.Fatal("missing source envelope or private response policy")
	}
	return page, w
}

// Publish real version-1 registry bytes; synthetic paths deliberately need not
// exist. Only tests that exercise Add/Resolve touch selected project content.
func paTargetsRegistry(t *testing.T, f *paServerFixture, rows []projects.RegisteredPlace) {
	t.Helper()
	if rows == nil {
		rows = []projects.RegisteredPlace{}
	}
	raw, err := json.Marshal(struct {
		Version int                        `json:"version"`
		Places  []projects.RegisteredPlace `json:"places"`
	}{1, rows})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.s.cfg.Dir, projects.PlaceRegistryFile), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func paTargetsRows(t *testing.T, n int) []projects.RegisteredPlace {
	t.Helper()
	base := t.TempDir()
	rows := make([]projects.RegisteredPlace, n)
	for i := range rows {
		rows[i] = projects.RegisteredPlace{Path: filepath.Join(base, fmt.Sprintf("parent-%03d", i), "same-name"), AddedAt: int64(i + 1), ProjectKey: filepath.Join(base, fmt.Sprintf("canonical-%d", i))}
	}
	return rows
}

func TestProjectAssistantTargetsDefaultOff(t *testing.T) {
	f := newPAServerFixture(t, true)
	paAssertHTTPFailure(t, f.request(t, "GET", "/v1/project-assistant-targets?machine_id="+f.key.MachineID+"&cursor=&page_size=100", f.local, nil), 403, pa.Disabled)
	if f.providerCalls.Load() != 0 {
		t.Fatal("metadata initialized a provider")
	}
	var tables int
	if err := f.s.store.ProjectAssistantDatabase().QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name LIKE 'project_assistant_%'`).Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("default-off catalog installed feature tables: %d %v", tables, err)
	}
	if f.s.projectAssistants.host != nil || f.s.broker.AssistantAuthority != nil {
		t.Fatal("default-off catalog enabled runtime")
	}
}

func TestProjectAssistantTargetsMetadataAndSelectedResolution(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	root := t.TempDir()
	repo := filepath.Join(root, "repo", "same-name")
	worktree := filepath.Join(root, "linked", "same-name")
	plain := filepath.Join(root, "plain", "same-name")
	for _, path := range []string{repo, filepath.Dir(worktree), plain} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", repo}, {"-C", repo, "-c", "user.name=Synthetic", "-c", "user.email=synthetic@example.invalid", "commit", "--allow-empty", "-m", "Synthetic fixture"}, {"-C", repo, "worktree", "add", "-b", "synthetic-linked", worktree}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %v %s", err, out)
		}
	}
	rows, err := projects.OpenPlaceRegistry(f.s.cfg.Dir).Add([]string{repo, worktree, plain}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	page, _ := paTargetsRead(t, f, f.readonly, "", 100)
	if len(page.Items) != len(rows) || page.Source.Writable || page.Source.Access != contract.ProjectAssistantAccessInteractive || len(page.Source.Capabilities) != 0 {
		t.Fatalf("configuration read changed authority or lost choices: %+v", page)
	}
	if !reflect.DeepEqual(page.Agents, []contract.ProjectAssistantAgent{contract.ProjectAssistantAgentClaude, contract.ProjectAssistantAgentCodex}) {
		t.Fatal(page.Agents)
	}
	seen := map[string]bool{}
	for _, item := range page.Items {
		if seen[item.Target.ProjectID] || item.Target.MachineID != f.key.MachineID {
			t.Fatal("collapsed duplicate labels or changed machine")
		}
		seen[item.Target.ProjectID] = true
	}
	for _, row := range rows {
		if !seen[projects.PlaceID(row.Path)] {
			t.Fatal("registered repository/place/worktree missing")
		}
	}
	// Metadata survives a deleted selected directory, while Create continues
	// through the existing resolver and rejects it.
	if err := os.RemoveAll(f.project); err != nil {
		t.Fatal(err)
	}
	deleted, _ := paTargetsRead(t, f, f.local, "", 100)
	if deleted.Revision != page.Revision || len(deleted.Items) != len(page.Items) {
		t.Fatal("catalog inspected directory existence")
	}
	paAssertHTTPFailure(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()), 404, pa.NotFound)
	old := f.request(t, "GET", "/v1/projects", f.local, nil)
	paServerOK(t, old)
	var legacy struct {
		Catalog struct {
			Projects []json.RawMessage `json:"projects"`
		} `json:"catalog"`
	}
	if json.Unmarshal(old.Body.Bytes(), &legacy) != nil || legacy.Catalog.Projects == nil || len(legacy.Catalog.Projects) != 0 {
		t.Fatalf("retired catalog changed: %s", old.Body)
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("configuration catalog probed a provider")
	}
}

func TestProjectAssistantTargetsPaginationAndCatalogRevision(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	rows := paTargetsRows(t, 101)
	paTargetsRegistry(t, f, rows)
	first, _ := paTargetsRead(t, f, f.local, "", 100)
	if len(first.Items) != 100 || !first.HasMore || first.NextCursor == "" {
		t.Fatal("first page boundary", len(first.Items), first.HasMore)
	}
	second, _ := paTargetsRead(t, f, f.local, first.NextCursor, 100)
	if len(second.Items) != 1 || second.HasMore || second.NextCursor != "" || second.Revision != first.Revision {
		t.Fatal("last page boundary")
	}
	ids := []string{}
	for _, item := range append(first.Items, second.Items...) {
		ids = append(ids, item.Target.ProjectID)
	}
	want := []string{}
	for _, row := range rows {
		want = append(want, projects.PlaceID(row.Path))
	}
	sort.Strings(want)
	if !reflect.DeepEqual(ids, want) {
		t.Fatal("pagination repeated, skipped, or reordered a registered choice")
	}
	// File order does not change the snapshot; metadata, addition and removal do.
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	paTargetsRegistry(t, f, rows)
	stable, _ := paTargetsRead(t, f, f.local, "", 100)
	if stable.Revision != first.Revision || stable.NextCursor != first.NextCursor {
		t.Fatal("registry order changed stable snapshot")
	}
	for _, mutate := range []func([]projects.RegisteredPlace) []projects.RegisteredPlace{
		func(r []projects.RegisteredPlace) []projects.RegisteredPlace { r[0].AddedAt++; return r },
		func(r []projects.RegisteredPlace) []projects.RegisteredPlace { r[0].ProjectKey += "-changed"; return r },
		func(r []projects.RegisteredPlace) []projects.RegisteredPlace { return r[1:] },
		func(r []projects.RegisteredPlace) []projects.RegisteredPlace {
			return append(r, projects.RegisteredPlace{Path: filepath.Join(t.TempDir(), "new"), AddedAt: 500})
		},
	} {
		changed := mutate(append([]projects.RegisteredPlace{}, rows...))
		paTargetsRegistry(t, f, changed)
		paAssertHTTPFailure(t, f.request(t, "GET", paTargetsURL(f.key.MachineID, first.NextCursor, 100), f.local, nil), 410, pa.CursorExpired)
	}
	paTargetsRegistry(t, f, nil)
	empty, _ := paTargetsRead(t, f, f.local, "", 100)
	if empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != "" || empty.HasMore {
		t.Fatal("empty registry did not produce one complete empty snapshot")
	}
}

func TestProjectAssistantTargetsStrictQueryAndCursor(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	paTargetsRegistry(t, f, paTargetsRows(t, 3))
	first, _ := paTargetsRead(t, f, f.local, "", 1)
	for _, query := range []string{
		"", "machine_id=" + f.key.MachineID, "machine_id=" + f.key.MachineID + "&cursor=&page_size=0",
		"machine_id=" + f.key.MachineID + "&cursor=&page_size=101", "machine_id=" + f.key.MachineID + "&cursor=&page_size=null",
		"machine_id=" + f.key.MachineID + "&cursor=&page_size=-1", "machine_id=" + f.key.MachineID + "&cursor=&page_size=1.0",
		"machine_id=" + f.key.MachineID + "&cursor=&page_size=1&extra=true", "machine_id=" + f.key.MachineID + "&cursor=&page_size=1&page_size=1",
		"machine_id=" + f.key.MachineID + "&cursor=&cursor=&page_size=1", "machine_id=" + f.key.MachineID + "&machine_id=" + f.key.MachineID + "&cursor=&page_size=1",
		"machine_id=" + f.key.MachineID + "&cursor=%XX&page_size=1", "machine_id=" + f.key.MachineID + "&cursor=&page_size=1;extra=1",
	} {
		paAssertHTTPFailure(t, f.request(t, "GET", "/v1/project-assistant-targets?"+query, f.local, nil), 400, pa.Invalid)
	}
	for _, machine := range []string{"", "null", "another-machine"} {
		paAssertHTTPFailure(t, f.request(t, "GET", paTargetsURL(machine, "", 1), f.local, nil), 403, pa.ScopeDenied)
	}
	for _, cursor := range []string{"null", "[]", "e30", first.NextCursor + "=", first.NextCursor[:len(first.NextCursor)-1], strings.Repeat("a", len(first.NextCursor))} {
		paAssertHTTPFailure(t, f.request(t, "GET", paTargetsURL(f.key.MachineID, cursor, 1), f.local, nil), 410, pa.CursorExpired)
	}
	// Even a correctly authenticated framing must name a real nonterminal row.
	data, _ := base64.RawURLEncoding.DecodeString(first.NextCursor)
	var binding [32]byte
	copy(binding[:], data)
	secret, err := f.s.gate().files.ReadLocalToken()
	if err != nil {
		t.Fatal(err)
	}
	all, _ := paTargetsRead(t, f, f.local, "", 100)
	for _, after := range []string{strings.Repeat("f", 16), all.Items[len(all.Items)-1].Target.ProjectID} {
		cursor := paTargetsCursor([]byte(secret), binding, after)
		paAssertHTTPFailure(t, f.request(t, "GET", paTargetsURL(f.key.MachineID, cursor, 1), f.local, nil), 410, pa.CursorExpired)
	}
	paAssertHTTPFailure(t, f.request(t, "POST", paTargetsURL(f.key.MachineID, "", 1), f.local, `{}`), 400, pa.Invalid)
	// "projects" remains a legal Bot ID; the metadata endpoint consumes none.
	create := f.create()
	create.Command.Key.BotID = "projects"
	if _, err := projects.OpenPlaceRegistry(f.s.cfg.Dir).Add([]string{f.project}, time.Now()); err != nil {
		t.Fatal(err)
	}
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, create))
}

func TestProjectAssistantTargetsPrincipalGrantMachineAndWitness(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	rows := paTargetsRows(t, 3)
	paTargetsRegistry(t, f, rows)
	first, _ := paTargetsRead(t, f, f.lan, "", 1)
	paAssertHTTPFailure(t, f.request(t, "GET", paTargetsURL(f.key.MachineID, first.NextCursor, 1), f.readonly, nil), 410, pa.CursorExpired)
	id := f.s.gate().auth.Verify(f.lan).Device
	if err := f.s.gate().auth.SetPermission(id, true); err != nil {
		t.Fatal(err)
	}
	paAssertHTTPFailure(t, f.request(t, "GET", paTargetsURL(f.key.MachineID, first.NextCursor, 1), f.lan, nil), 410, pa.CursorExpired)
	readonly, _ := paTargetsRead(t, f, f.lan, "", 1)
	if readonly.Source.Writable {
		t.Fatal("catalog conferred write authority")
	}
	other := newPAServerFixture(t, true)
	other.identity.MachineID = "second-synthetic-machine"
	other.key.MachineID = other.identity.MachineID
	if err := adaptercloud.NewIdentityStore(filepath.Join(other.s.cfg.Dir, cloudkeys.DirName)).Save(other.identity); err != nil {
		t.Fatal(err)
	}
	other.enable(t)
	paTargetsRegistry(t, other, rows)
	paAssertHTTPFailure(t, other.request(t, "GET", paTargetsURL(other.key.MachineID, first.NextCursor, 1), other.local, nil), 410, pa.CursorExpired)
	unpaired := newPAServerFixture(t, false)
	paAssertHTTPFailure(t, unpaired.request(t, "GET", paTargetsURL(unpaired.key.MachineID, "", 1), unpaired.local, nil), 403, pa.Unpaired)
	// A synthesized verdict cannot replace the gate's private request witness.
	for _, witness := range []access{{gate: f.s.gate(), verdict: auth.Verdict{Allowed: true, Device: id, Caps: auth.Caps{auth.Read}}}, {gate: other.s.gate(), verdict: other.s.gate().auth.Verify(other.local)}} {
		r := httptest.NewRequest("GET", paTargetsURL(f.key.MachineID, "", 1), nil)
		r = r.WithContext(context.WithValue(r.Context(), accessKey{}, witness))
		w := httptest.NewRecorder()
		f.s.projectAssistantTargetsRoute(w, r)
		if w.Code != 403 || strings.Contains(w.Body.String(), "same-name") {
			t.Fatal("invalid witness disclosed metadata", w.Code, w.Body)
		}
	}
}

func TestProjectAssistantTargetsCloudGrantsAndPublicationFence(t *testing.T) {
	for _, change := range []string{"device-revoke", "device-downgrade", "grant-revoke", "grant-downgrade", "catalog-change", "identity-change", "feature-disable"} {
		t.Run(change, func(t *testing.T) {
			f := newPAServerFixture(t, true)
			f.enable(t)
			paTargetsRegistry(t, f, paTargetsRows(t, 3))
			f.pin(t, "viewer", adaptercloud.ScopeOperate, true)
			f.pin(t, "reader", adaptercloud.ScopeReadOnly, true)
			path := paTargetsURL(f.key.MachineID, "", 1)
			reader := f.cloud(t, "reader", "GET", path, nil)
			if reader.Status != 200 || !bytes.Contains(reader.Body, []byte(`"writable":false`)) {
				t.Fatalf("read-only grant metadata: %d %s", reader.Status, reader.Body)
			}
			if missing := f.cloud(t, "missing", "GET", path, nil); missing.Status != 403 {
				t.Fatal("unpaired viewer read catalog")
			}
			f.s.projectAssistantTargetsBeforePublish = func() {
				switch change {
				case "device-revoke":
					if err := f.s.gate().auth.Revoke(f.s.gate().auth.Verify(f.lan).Device); err != nil {
						t.Fatal(err)
					}
				case "device-downgrade":
					if err := f.s.gate().auth.SetPermission(f.s.gate().auth.Verify(f.lan).Device, true); err != nil {
						t.Fatal(err)
					}
				case "grant-revoke":
					if _, err := adaptercloud.NewPinnedStore(filepath.Join(f.s.cfg.Dir, cloudkeys.DirName)).Revoke("viewer", time.Now()); err != nil {
						t.Fatal(err)
					}
				case "grant-downgrade":
					f.pin(t, "viewer", adaptercloud.ScopeReadOnly, true)
				case "catalog-change":
					paTargetsRegistry(t, f, paTargetsRows(t, 2))
				case "identity-change":
					i := f.identity
					i.MachineID = "changed-source"
					if err := adaptercloud.NewIdentityStore(filepath.Join(f.s.cfg.Dir, cloudkeys.DirName)).Save(i); err != nil {
						t.Fatal(err)
					}
				case "feature-disable":
					paServerOK(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":false}`))
				}
			}
			var body []byte
			var status int
			if strings.HasPrefix(change, "grant-") {
				r := f.cloud(t, "viewer", "GET", path, nil)
				status, body = r.Status, r.Body
			} else {
				r := f.request(t, "GET", path, f.lan, nil)
				status, body = r.Code, r.Body.Bytes()
			}
			if status < 400 || bytes.Contains(body, []byte(`"items"`)) || bytes.Contains(body, []byte("same-name")) {
				t.Fatalf("published content after %s: %d %s", change, status, body)
			}
			if f.providerCalls.Load() != 0 {
				t.Fatal("publication fence initialized a provider")
			}
		})
	}
}

func TestProjectAssistantTargetsCloudCursorGrantsAndRetainedHistory(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	paTargetsRegistry(t, f, paTargetsRows(t, 3))
	f.pin(t, "guest-a", adaptercloud.ScopeOperate, true)
	f.pin(t, "guest-b", adaptercloud.ScopeOperate, true)
	first := f.cloud(t, "guest-a", "GET", paTargetsURL(f.key.MachineID, "", 1), nil)
	var page contract.ProjectAssistantProjectsResponse
	if first.Status != 200 || json.Unmarshal(first.Body, &page) != nil || page.NextCursor == "" {
		t.Fatal("missing Cloud cursor")
	}
	path := paTargetsURL(f.key.MachineID, page.NextCursor, 1)
	if other := f.cloud(t, "guest-b", "GET", path, nil); other.Status != 410 {
		t.Fatal("another principal reused cursor")
	}
	f.pin(t, "guest-a", adaptercloud.ScopeReadOnly, true)
	if downgraded := f.cloud(t, "guest-a", "GET", path, nil); downgraded.Status != 410 {
		t.Fatal("downgraded grant reused cursor")
	}
	if _, err := adaptercloud.NewPinnedStore(filepath.Join(f.s.cfg.Dir, cloudkeys.DirName)).Revoke("guest-a", time.Now()); err != nil {
		t.Fatal(err)
	}
	if revoked := f.cloud(t, "guest-a", "GET", path, nil); revoked.Status != 403 {
		t.Fatal("revoked grant read catalog")
	}
	paServerOK(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":false}`))
	retained, _ := paTargetsRead(t, f, f.readonly, "", 100)
	if retained.Source.Access != contract.ProjectAssistantAccessHistoryOnly || retained.Source.Writable || len(retained.Items) != 3 {
		t.Fatal("retained-history metadata changed source access")
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("retained catalog initialized provider")
	}
}

func TestProjectAssistantTargetsInvalidRegistryRefusesContent(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	validPath := filepath.Join(t.TempDir(), "same")
	paTargetsRegistry(t, f, []projects.RegisteredPlace{{Path: validPath, AddedAt: 1}})
	valid, _ := paTargetsRead(t, f, f.local, "", 100)
	if len(valid.Items) != 1 || valid.Items[0].Target.ProjectID != projects.PlaceID(validPath) {
		t.Fatal("valid path control failed before malformed registry cases")
	}
	samePath, err := json.Marshal(validPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"version":1,"places":null}`, `{"version":1,"places":[`,
		fmt.Sprintf(`{"version":1,"places":[{"path":%s,"added_at":1},{"path":%s,"added_at":2}]}`, samePath, samePath),
		fmt.Sprintf(`{"version":1,"places":[{"path":%s,"added_at":1}],"hidden":[%s]}`, samePath, samePath),
	} {
		if err := os.WriteFile(filepath.Join(f.s.cfg.Dir, projects.PlaceRegistryFile), []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		w := f.request(t, "GET", paTargetsURL(f.key.MachineID, "", 100), f.local, nil)
		paAssertHTTPFailure(t, w, 409, pa.StaleVersion)
		if bytes.Contains(w.Body.Bytes(), []byte(`"items"`)) {
			t.Fatal("invalid registry produced partial success")
		}
	}
	missing := filepath.Join(t.TempDir(), "not-present")
	paTargetsRegistry(t, f, []projects.RegisteredPlace{{Path: missing, AddedAt: 1}})
	if _, err := projects.OpenPlaceRegistry(f.s.cfg.Dir).Remove([]string{missing}); err != nil {
		t.Fatal(err)
	}
	page, _ := paTargetsRead(t, f, f.local, "", 100)
	if len(page.Items) != 0 {
		t.Fatal("hidden project appeared in config options")
	}
}

type paTargetsBudgetRouter struct{ body []byte }

func (r paTargetsBudgetRouter) Do(context.Context, cloudops.LocalRequest) (cloudops.LocalResponse, error) {
	return cloudops.LocalResponse{Status: 200, Body: r.body}, nil
}

func TestProjectAssistantTargetsCloudSerializerBudget(t *testing.T) {
	// Exercise the actual existing Answer/payload path through an already
	// registered read word. This task does not register a new Cloud word.
	raw := json.RawMessage(`{"contract_version":"project-assistants.v1","html":"<>&\u2028\u2029"}` + "\n")
	bridge := cloudops.Bridge{MachineID: "synthetic-machine", Router: paTargetsBudgetRouter{raw}}
	answer := bridge.Handle(context.Background(), cloudops.Command{Channel: "ctl/synthetic-machine", Class: cloudops.ClassCtl, Plaintext: []byte(`{"type":"project-assistants.status","session":"__clawdline_machine__","request":"budget"}`)})
	if !answer.Published() || answer.Status != 200 {
		t.Fatalf("serializer fixture refused: %+v", answer)
	}
	size, err := cloudops.ProjectAssistantReadReplySize(answer.Name, answer.Status, raw)
	if err != nil || size != len(answer.Payload) {
		t.Fatalf("measurement %d != actual payload %d: %v", size, len(answer.Payload), err)
	}
	if size <= len(raw) || !bytes.Contains(answer.Payload, []byte(`\u003c`)) {
		t.Fatal("measurement omitted HTML escaping or wrapper")
	}
	for _, invalid := range []json.RawMessage{nil, {}, []byte(" "), []byte("{"), []byte("{} trailing")} {
		if n, err := cloudops.ProjectAssistantReadReplySize("project-assistants.projects", 200, invalid); err == nil || n != 0 {
			t.Fatal("invalid JSON measured as a successful empty reply")
		}
	}
}

func TestProjectAssistantTargetsCompleteByteLimitedRows(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	// Ampersands are legal filenames on every supported OS, and JSON HTML
	// escaping multiplies their bytes without needing a physical directory.
	label := strings.Repeat("&", cloudops.ReadPlaintextBytesLimit/6/2)
	base := t.TempDir()
	rows := []projects.RegisteredPlace{{Path: filepath.Join(base, "one", label), AddedAt: 1}, {Path: filepath.Join(base, "two", label), AddedAt: 2}, {Path: filepath.Join(base, "three", "small"), AddedAt: 3}}
	paTargetsRegistry(t, f, rows)
	seen := map[string]bool{}
	cursor := ""
	pageCount := 0
	for pages := 0; ; pages++ {
		if pages > len(rows) {
			t.Fatal("cursor did not progress")
		}
		page, w := paTargetsRead(t, f, f.local, cursor, 100)
		pageCount++
		n, err := cloudops.ProjectAssistantReadReplySize("read:"+strings.Repeat("<", 128), 200, w.Body.Bytes())
		if err != nil || n > cloudops.ReadPlaintextBytesLimit || w.Body.Len() > cloudops.ReadPlaintextBytesLimit {
			t.Fatal("overflowing successful reply", n, err)
		}
		if len(page.Items) == 0 {
			t.Fatal("byte budget fabricated empty page")
		}
		for _, item := range page.Items {
			if seen[item.Target.ProjectID] {
				t.Fatal("byte budget repeated a choice")
			}
			seen[item.Target.ProjectID] = true
			if item.Label != label && item.Label != "small" {
				t.Fatal("byte budget truncated a label")
			}
		}
		if !page.HasMore {
			if page.NextCursor != "" {
				t.Fatal("terminal page has cursor")
			}
			break
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			t.Fatal("byte budget failed to advance")
		}
		cursor = page.NextCursor
	}
	if len(seen) != len(rows) {
		t.Fatal("byte budget lost a choice")
	}
	if pageCount < 2 {
		t.Fatal("fixture did not force a byte-limited page")
	}
	// One indivisible label cannot fit. No partial Project escapes in refusal.
	paTargetsRegistry(t, f, []projects.RegisteredPlace{{Path: filepath.Join(base, strings.Repeat("&", cloudops.ReadPlaintextBytesLimit/6+1)), AddedAt: 1}})
	w := f.request(t, "GET", paTargetsURL(f.key.MachineID, "", 100), f.local, nil)
	paAssertHTTPFailure(t, w, 429, pa.CapacityFull)
	if bytes.Contains(w.Body.Bytes(), []byte(`"items"`)) || w.Body.Len() > 1024 {
		t.Fatal("capacity refusal leaked partial Project")
	}
	paTargetsRegistry(t, f, paTargetsRows(t, int(capacity.Default(capacity.PlacesRegistered))+1))
	paAssertHTTPFailure(t, f.request(t, "GET", paTargetsURL(f.key.MachineID, "", 100), f.local, nil), 429, pa.CapacityFull)
}

func TestProjectAssistantTargetsWrapperCursorAndMetadataBudget(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	paTargetsRegistry(t, f, nil)
	base, _ := paTargetsRead(t, f, f.local, "", 100)
	largePath := filepath.Join(t.TempDir(), "large")
	base.Items = []contract.ProjectAssistantProjectOption{{Target: contract.ProjectAssistantProjectSelection{MachineID: f.key.MachineID, ProjectID: projects.PlaceID(largePath)}}}
	empty, _, err := paTargetsEncodePage(base)
	if err != nil {
		t.Fatal(err)
	}
	base.Items[0].Label = strings.Repeat("a", cloudops.ReadPlaintextBytesLimit-len(empty)-20)
	raw, fits, err := paTargetsEncodePage(base)
	if err != nil || len(raw) > cloudops.ReadPlaintextBytesLimit || fits {
		t.Fatal("fixture failed to isolate Cloud wrapper budget")
	}
	paTargetsRegistry(t, f, []projects.RegisteredPlace{{Path: filepath.Join(t.TempDir(), base.Items[0].Label), AddedAt: 1}})
	paAssertHTTPFailure(t, f.request(t, "GET", paTargetsURL(f.key.MachineID, "", 100), f.local, nil), 429, pa.CapacityFull)
	// A complete single-row reply fits exactly when measured with the actual
	// serializer. Adding only a continuation cursor must make it stop fitting.
	base.Items[0].Label = ""
	_, _, err = paTargetsEncodePage(base)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(base)
	n, err := cloudops.ProjectAssistantReadReplySize("read:"+strings.Repeat("<", 128), 200, raw)
	if err != nil {
		t.Fatal(err)
	}
	base.Items[0].Label = strings.Repeat("b", cloudops.ReadPlaintextBytesLimit-n)
	if _, fits, err := paTargetsEncodePage(base); err != nil || !fits {
		t.Fatal("exact complete wrapper did not fit", err)
	}
	base.Items[0].Label += "b"
	if _, fits, err := paTargetsEncodePage(base); err != nil || fits {
		t.Fatal("one byte beyond the worst legal correlation wrapper still fit", err)
	}
	base.Items[0].Label = strings.TrimSuffix(base.Items[0].Label, "b")
	base.HasMore = true
	base.NextCursor = paTargetsCursor([]byte("synthetic-key"), [32]byte{}, projects.PlaceID(largePath))
	if _, fits, err := paTargetsEncodePage(base); err != nil || fits {
		t.Fatal("cursor overhead was omitted", err)
	}
	// Metadata alone can exceed the existing reply budget without any rows.
	base.Items = []contract.ProjectAssistantProjectOption{}
	base.HasMore, base.NextCursor = false, ""
	base.Source.MachineID = strings.Repeat("<", cloudops.ReadPlaintextBytesLimit/6+1)
	if _, fits, err := paTargetsEncodePage(base); err != nil || fits {
		t.Fatal("metadata overhead was omitted", err)
	}
}

func TestProjectAssistantTargetsLongestCorrelationNameBudget(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	paTargetsRegistry(t, f, nil)
	base, _ := paTargetsRead(t, f, f.local, "", 100)
	largePath := filepath.Join(t.TempDir(), "large")
	base.Items = []contract.ProjectAssistantProjectOption{{Target: contract.ProjectAssistantProjectSelection{MachineID: f.key.MachineID, ProjectID: projects.PlaceID(largePath)}}}
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	longest := "read:" + strings.Repeat("<", 128)
	n, err := cloudops.ProjectAssistantReadReplySize(longest, 200, raw)
	if err != nil {
		t.Fatal(err)
	}
	// A full response below the literal-word estimate exceeds the actual
	// longest legal correlation-name wrapper. The margin absorbs timestamp
	// fraction formatting without approximating any response serialization.
	base.Items[0].Label = strings.Repeat("z", cloudops.ReadPlaintextBytesLimit-n+32)
	raw, err = json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := cloudops.ProjectAssistantReadReplySize(longest, 200, raw)
	if err != nil {
		t.Fatal(err)
	}
	literal, err := cloudops.ProjectAssistantReadReplySize("project-assistants.projects", 200, raw)
	if err != nil || literal > cloudops.ReadPlaintextBytesLimit || actual <= cloudops.ReadPlaintextBytesLimit {
		t.Fatal("fixture does not isolate correlation-name underestimate")
	}
	paTargetsRegistry(t, f, []projects.RegisteredPlace{{Path: filepath.Join(t.TempDir(), base.Items[0].Label), AddedAt: 1}})
	w := f.request(t, "GET", paTargetsURL(f.key.MachineID, "", 100), f.local, nil)
	if w.Code != 429 {
		measured, _ := cloudops.ProjectAssistantReadReplySize(longest, w.Code, w.Body.Bytes())
		t.Fatalf("HTTP %d published a reply requiring %d Cloud bytes (limit %d); literal estimate %d", w.Code, measured, cloudops.ReadPlaintextBytesLimit, literal)
	}
	paAssertHTTPFailure(t, w, 429, pa.CapacityFull)
	if bytes.Contains(w.Body.Bytes(), []byte(`"items"`)) {
		t.Fatal("near-boundary refusal published partial Project")
	}
}
