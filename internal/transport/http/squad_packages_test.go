package http

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/domain/squadpack"
)

func TestPackageRoutesKeepPreviewAndPublicExportReadableButProtectAdoptionAndPrivateExport(t *testing.T) {
	f := newSquadFixture(t)
	archive, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", "ai-squad-example.zip"))
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(archive)
	previewBody, _ := json.Marshal(map[string]any{"archive_base64": encoded, "scope_id": "global"})
	if status, _ := f.ask("POST", "/v1/squad-packages/preview", "", string(previewBody), ""); status != 401 {
		t.Fatalf("anonymous preview = %d", status)
	}
	status, raw := f.ask("POST", "/v1/squad-packages/preview", f.reader, string(previewBody), "")
	if status != 200 {
		t.Fatalf("paired reader preview = %d: %s", status, raw)
	}
	status, raw = f.ask("POST", "/v1/squad-packages/preview", f.sender, string(previewBody), "")
	if status != 200 {
		t.Fatalf("writer preview = %d: %s", status, raw)
	}
	var preview struct {
		ArchiveDigest  string `json:"archive_digest"`
		CatalogVersion int64  `json:"catalog_version"`
		PreviewDigest  string `json:"preview_digest"`
		PreviewToken   string `json:"preview_token"`
		Source         string `json:"source"`
		License        string `json:"license"`
	}
	if err := json.Unmarshal([]byte(raw), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.ArchiveDigest == "" || preview.PreviewDigest == "" || preview.PreviewToken == "" ||
		preview.Source == "" || preview.License == "" {
		t.Fatalf("incomplete preview: %+v", preview)
	}
	adoptBody, _ := json.Marshal(map[string]any{"archive_base64": encoded,
		"archive_digest": preview.ArchiveDigest, "preview_digest": preview.PreviewDigest,
		"preview_token": preview.PreviewToken, "catalog_version": preview.CatalogVersion,
		"scope_id": "global", "choices": map[string]string{}, "private_scopes": []string{},
		"confirm_private": false})
	if status, _ := f.ask("POST", "/v1/squad-packages/adopt", f.reader, string(adoptBody), "adopt-1"); status != 403 {
		t.Fatalf("reader adoption = %d", status)
	}
	status, raw = f.ask("POST", "/v1/squad-packages/adopt", f.sender, string(adoptBody), "adopt-1")
	if status != 200 || !strings.Contains(raw, `"catalog_version":1`) {
		t.Fatalf("adoption = %d: %s", status, raw)
	}
	if status, body := f.ask("GET", "/v1/squad/catalog", f.reader, "", ""); status != 200 || !strings.Contains(body, "example.squad.persona.writer") {
		t.Fatalf("catalog after package adoption = %d: %s", status, body)
	}
	if status, body := f.ask("GET", "/v1/squad/settings", f.reader, "", ""); status != 200 {
		t.Fatalf("settings after package adoption = %d: %s", status, body)
	}
	if retry, body := f.ask("POST", "/v1/squad-packages/adopt", f.sender, string(adoptBody), "adopt-1"); retry != 200 || body != raw {
		t.Fatalf("same adoption replay = %d: %s", retry, body)
	}
	var changed map[string]any
	_ = json.Unmarshal(adoptBody, &changed)
	changed["archive_digest"] = "sha256:different"
	changedBody, _ := json.Marshal(changed)
	if status, body := f.ask("POST", "/v1/squad-packages/adopt", f.sender, string(changedBody), "adopt-1"); status != 409 || !strings.Contains(body, "idempotency_conflict") {
		t.Fatalf("changed replay = %d: %s", status, body)
	}
	publicBody := `{"private_scopes":[],"confirm_private":false}`
	status, raw = f.ask("POST", "/v1/squad-packages/export", f.reader, publicBody, "")
	if status != 200 {
		t.Fatalf("public export = %d: %s", status, raw)
	}
	var exported struct {
		ArchiveBase64 string `json:"archive_base64"`
		MIMEType      string `json:"mime_type"`
	}
	if err := json.Unmarshal([]byte(raw), &exported); err != nil {
		t.Fatal(err)
	}
	public, err := base64.StdEncoding.DecodeString(exported.ArchiveBase64)
	if err != nil || exported.MIMEType != "application/zip" {
		t.Fatalf("export envelope: %v, %s", err, exported.MIMEType)
	}
	parsed, err := squadpack.Parse(public)
	if err != nil || len(parsed.Manifest.Personas) != 2 || len(parsed.Manifest.Private) != 0 {
		t.Fatalf("exported package: %v", err)
	}
	scope, ok := projects.ResolveScope(f.paths["a"])
	if !ok {
		t.Fatal("fixture Project scope missing")
	}
	privateBody, _ := json.Marshal(map[string]any{"private_scopes": []string{scope.ID}, "confirm_private": true})
	if status, _ := f.ask("POST", "/v1/squad-packages/export", f.reader, string(privateBody), ""); status != 403 {
		t.Fatalf("reader private export = %d", status)
	}
	if status, body := f.ask("POST", "/v1/squad-packages/export", f.sender, string(privateBody), ""); status != 200 {
		t.Fatalf("writer private export = %d: %s", status, body)
	}
}
