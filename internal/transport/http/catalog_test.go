package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/config"
)

func writeCatalogFixture(t *testing.T, root, language, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "catalogs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "catalogs", language+".json"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestStringsServesCompleteCatalogOrAtomicEnglishFallback(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAWDLINE_NEXT_WEB", root)
	english := `{"lang":"en","dir":"ltr","title":"Hello {name}","count":"One {n}\u001fMany {n}"}`
	selected := `{"lang":"zh-Hant","dir":"ltr","title":"你好 {name}","count":"一個 {n}\u001f多個 {n}"}`
	writeCatalogFixture(t, root, "en", english)
	writeCatalogFixture(t, root, "zh-Hant", selected)
	writeCatalogFixture(t, root, "zh-Hans", `{"lang":"zh-Hans","dir":"ltr","title":"你好 {name}","count":"一个 {n}\u001f多个 {n}"}`)
	settingsDir := t.TempDir()
	if _, err := nextconfig.Open(settingsDir).Set(map[string]any{"product_language": "de"}); err != nil {
		t.Fatal(err)
	}
	// This endpoint does not infer the daemon's product preference; the
	// browser passes its own resolved tag explicitly.
	s := &Server{cfg: config.Config{Dir: settingsDir}}
	for _, tc := range []struct {
		name, query, body, wantLanguage string
		status                          int
	}{
		{"default", "", "", "en", 200},
		{"selected", "?lang=zh-Hant", "", "zh-Hant", 200},
		{"region alias", "?lang=zh-MO", "", "zh-Hant", 200},
		{"Traditional script before region", "?lang=zh-Hant-CN", "", "zh-Hant", 200},
		{"script before region", "?lang=zh-Hans-TW", "", "zh-Hans", 200},
		{"unknown script", "?lang=zh-Latn-TW", "", "en", 200},
		{"unknown", "?lang=it", "", "en", 200},
		{"underscore is malformed for API", "?lang=zh_MO", "", "", 400},
		{"malformed", "?lang=../../en", "", "", 400},
		{"empty", "?lang=", "", "", 400},
		{"corrupt", "?lang=zh-Hant", `{`, "en", 200},
		{"missing metadata", "?lang=zh-Hant", `{"title":"你好 {name}","count":"一個 {n}\u001f多個 {n}"}`, "en", 200},
		{"missing key", "?lang=zh-Hant", `{"lang":"zh-Hant","dir":"ltr","title":"你好 {name}"}`, "en", 200},
		{"wrong placeholder", "?lang=zh-Hant", `{"lang":"zh-Hant","dir":"ltr","title":"你好 {other}","count":"一個 {n}\u001f多個 {n}"}`, "en", 200},
		{"broken placeholder", "?lang=zh-Hant", `{"lang":"zh-Hant","dir":"ltr","title":"你好 {name", "count":"一個 {n}\u001f多個 {n}"}`, "en", 200},
		{"wrong plural", "?lang=zh-Hant", `{"lang":"zh-Hant","dir":"ltr","title":"你好 {name}","count":"一個 {n}"}`, "en", 200},
		{"injected markup", "?lang=zh-Hant", `{"lang":"zh-Hant","dir":"ltr","title":"你好 {name}<script>","count":"一個 {n}\u001f多個 {n}"}`, "en", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.body != "" {
				writeCatalogFixture(t, root, "zh-Hant", tc.body)
			}
			defer writeCatalogFixture(t, root, "zh-Hant", selected)
			rec := httptest.NewRecorder()
			s.strings(rec, httptest.NewRequest(http.MethodGet, "/v1/strings"+tc.query, nil))
			if rec.Code != tc.status {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if tc.status != 200 {
				if !strings.Contains(rec.Body.String(), `"error":"bad_request"`) {
					t.Fatalf("wrong refusal: %s", rec.Body.String())
				}
				return
			}
			var got map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got["lang"] != tc.wantLanguage || got["dir"] != "ltr" || len(got) != 4 {
				t.Fatalf("catalog = %#v", got)
			}
			if tc.wantLanguage == "en" && got["title"] != "Hello {name}" {
				t.Fatalf("mixed catalog: %#v", got)
			}
		})
	}
}

func TestStringsIsReadableBeforePairing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAWDLINE_NEXT_WEB", root)
	writeCatalogFixture(t, root, "en", `{"lang":"en","dir":"ltr","title":"Ready"}`)
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	rec := httptest.NewRecorder()
	s.gate().wrap(http.HandlerFunc(s.strings)).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7727/v1/strings", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"lang":"en"`) {
		t.Fatalf("unpaired catalog = %d %s", rec.Code, rec.Body.String())
	}
}

func TestSecondaryCatalogKeepsItsActualLanguageWhenOnlyNewKeysAreMissing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAWDLINE_NEXT_WEB", root)
	writeCatalogFixture(t, root, "en", `{"lang":"en","dir":"ltr","old":"Hello {name}","new":"New item"}`)
	writeCatalogFixture(t, root, "ja", `{"lang":"ja","dir":"ltr","old":"こんにちは {name}"}`)
	s := &Server{}
	read := func() map[string]string {
		t.Helper()
		rec := httptest.NewRecorder()
		s.strings(rec, httptest.NewRequest(http.MethodGet, "/v1/strings?lang=ja", nil))
		if rec.Code != 200 {
			t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
		}
		var got map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := read(); got["lang"] != "ja" || got["old"] != "こんにちは {name}" || got["new"] != "" {
		t.Fatalf("partial catalog was mixed or rejected: %#v", got)
	}
	writeCatalogFixture(t, root, "ja", `{"lang":"ja","dir":"ltr","old":"こんにちは {other}"}`)
	if got := read(); got["lang"] != "en" || got["old"] != "Hello {name}" || got["new"] != "New item" {
		t.Fatalf("bad existing key did not atomically fall back: %#v", got)
	}
	writeCatalogFixture(t, root, "ja", `{"lang":"fr","dir":"ltr","old":"こんにちは {name}"}`)
	if got := read(); got["lang"] != "en" {
		t.Fatalf("mislabelled catalog was accepted: %#v", got)
	}
	writeCatalogFixture(t, root, "ja", `{"lang":"ja","dir":"ltr","old":"こんにちは {name}","removed":"Obsolete"}`)
	if got := read(); got["lang"] != "en" || got["removed"] != "" {
		t.Fatalf("catalog with a removed English key was accepted: %#v", got)
	}
}

func TestCatalogKeepsInterpolationInItsPluralBranch(t *testing.T) {
	english := map[string]string{"count": "One\x1fMany {n}"}
	if !validCatalog(english, map[string]string{"count": "一個\x1f多個 {n}"}, true) {
		t.Fatal("valid plural translation was rejected")
	}
	for _, value := range []string{
		"一個 {n}\x1f多個", // The placeholder moved to the wrong branch.
		"一個\x1f多個",     // The placeholder disappeared.
		"一個\x1f",       // The second branch is empty.
	} {
		if validCatalog(english, map[string]string{"count": value}, true) {
			t.Errorf("accepted invalid plural translation %q", value)
		}
	}
}

func TestEnglishCatalogFailureIsTypedAndPageDoesNotEmbedBrokenCopy(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAWDLINE_NEXT_WEB", root)
	writeCatalogFixture(t, root, "en", `{`)
	rec := httptest.NewRecorder()
	(&Server{}).strings(rec, httptest.NewRequest(http.MethodGet, "/v1/strings", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"error":"catalog_unreadable"`) {
		t.Fatalf("catalog failure = %d %s", rec.Code, rec.Body.String())
	}
	if embedded := newPage(root).strings(); embedded != "" {
		t.Fatalf("broken English embedded: %s", embedded)
	}
	writeCatalogFixture(t, root, "en", `{"title":"Ready"}`)
	rec = httptest.NewRecorder()
	(&Server{}).strings(rec, httptest.NewRequest(http.MethodGet, "/v1/strings", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("English without metadata = %d %s", rec.Code, rec.Body.String())
	}
	writeCatalogFixture(t, root, "en", `{"lang":"en","dir":"ltr","title":"Ready"}`)
	if embedded := newPage(root).strings(); !strings.Contains(embedded, `"lang":"en"`) || !strings.Contains(embedded, `"title":"Ready"`) {
		t.Fatalf("English embed = %s", embedded)
	}
}

func TestHomeDocumentStartsHiddenInEnglishAndEmbedsOnlyValidEnglish(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte(`<html lang="zh-Hant" class="booting"><head></head><body><!-- clawdline:strings --></body></html>`), 0600); err != nil {
		t.Fatal(err)
	}
	writeCatalogFixture(t, root, "en", `{"lang":"en","dir":"ltr","title":"Ready"}`)
	rec := httptest.NewRecorder()
	newPage(root).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `<html lang="en" class="booting">`) || !strings.Contains(rec.Body.String(), `"title":"Ready"`) {
		t.Fatalf("home = %d %s", rec.Code, rec.Body.String())
	}
	writeCatalogFixture(t, root, "en", `{`)
	rec = httptest.NewRecorder()
	newPage(root).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != 200 || strings.Contains(rec.Body.String(), `window.__strings=`) {
		t.Fatalf("broken catalog embedded: %d %s", rec.Code, rec.Body.String())
	}
}

func TestProductEnglishDefaultLeavesAgentAndVoiceFallbackUnchanged(t *testing.T) {
	dir := t.TempDir()
	s := &Server{cfg: config.Config{Dir: dir}}
	if defaultCatalog != "en" || brokerLanguage(s) != "zh-Hant" {
		t.Fatalf("product=%q agent=%q", defaultCatalog, brokerLanguage(s))
	}
	follow := voiceFollow(nextconfig.Values{}, true)
	if len(follow) == 0 || follow[len(follow)-1].Tag != "zh-Hant" {
		t.Fatalf("voice fallback = %#v", follow)
	}
	if _, err := nextconfig.Open(dir).Set(map[string]any{"language": "ja", "voice_language": "ko", "product_language": "de"}); err != nil {
		t.Fatal(err)
	}
	if got := brokerLanguage(s); got != "ja" {
		t.Fatalf("agent language changed: %q", got)
	}
	values, err := nextconfig.Open(dir).Read()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := values.String("voice_language"); got != "ko" {
		t.Fatalf("voice language changed: %q", got)
	}
}
