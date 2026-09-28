package http

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWorkGateExportCarriesTheExactManifestBytes(t *testing.T) {
	s, _, project := sessionItemServer(t)
	item := personItem(t, s, project, "feature", "gate-export-item")
	rec := httptest.NewRecorder()
	s.workV2Route(rec, personWorkV2Request(http.MethodGet,
		"/v1/work/v2/items/"+item.ID+"/gate-export", "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("export: %d %s", rec.Code, rec.Body)
	}
	var answer struct {
		Document string `json:"document"`
		Manifest struct {
			ByteCount int    `json:"byte_count"`
			SHA256    string `json:"sha256"`
		} `json:"manifest"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if !json.Valid([]byte(answer.Document)) {
		t.Fatal("export document is not JSON text")
	}
	if len([]byte(answer.Document)) != answer.Manifest.ByteCount {
		t.Fatalf("document bytes: %d, manifest: %d", len([]byte(answer.Document)), answer.Manifest.ByteCount)
	}
	sum := sha256.Sum256([]byte(answer.Document))
	if hex.EncodeToString(sum[:]) != answer.Manifest.SHA256 {
		t.Fatal("the document string is not the exact manifest digest")
	}
}
