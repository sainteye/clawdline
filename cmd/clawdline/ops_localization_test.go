package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
)

func TestOpsCatalogNineLanguagesAndFormats(t *testing.T) {
	bundle := cliCatalogGroups["ops"]
	if len(bundle.english) < 94 {
		t.Fatalf("ops keys = %d, initial baseline is 94", len(bundle.english))
	}
	if len(bundle.baseline) < 94 {
		t.Fatalf("ops initial baseline has %d keys", len(bundle.baseline))
	}
	for _, language := range []string{"en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"} {
		translated, total := cliCatalogCoverage("ops", language)
		minimum := len(bundle.baseline)
		if language == "en" || language == "zh-Hant" {
			minimum = total
		}
		if translated < minimum || total < 94 {
			t.Errorf("%s catalog coverage = %d/%d", language, translated, total)
		}
		if language != "en" && (!validCLICatalog(bundle.english, bundle.locales[language], language == "zh-Hant") || !hasCLIBaseline(bundle.locales[language], bundle.baseline)) {
			t.Errorf("%s catalog has missing, empty or incompatible formats", language)
		}
	}
}

func TestOpsHumanCopyAndMachineValues(t *testing.T) {
	previous := commandLanguage
	t.Cleanup(func() { commandLanguage = previous })
	const id = "raw-device-ID_A9"
	const name = "Person's device name"
	list := contract.DeviceList{Devices: []contract.PairedDevice{{ID: id, Name: name, Created: 1_790_000_000}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/devices" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(list)
	}))
	defer server.Close()
	door := localDoor{base: server.URL, client: server.Client()}
	for _, tc := range []struct{ language, want string }{
		{"en", "this machine's own key"},
		{"zh-Hant", "本機自己的金鑰"},
		{"de", "eigenen Schlüssel"},
	} {
		commandLanguage = tc.language
		var out, errs bytes.Buffer
		if code := devicesList(&out, &errs, door, nil); code != 0 {
			t.Fatalf("%s devices exit %d: %s", tc.language, code, errs.String())
		}
		for _, raw := range []string{id, name, tc.want} {
			if !strings.Contains(out.String(), raw) {
				t.Errorf("%s device output lacks %q: %q", tc.language, raw, out.String())
			}
		}
		out.Reset()
		if code := devicesList(&out, &errs, door, []string{"--json"}); code != 0 {
			t.Fatalf("%s JSON exit %d: %s", tc.language, code, errs.String())
		}
		var raw map[string]any
		if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
			t.Fatalf("%s invalid JSON: %v", tc.language, err)
		}
		if !strings.Contains(out.String(), `"id": "`+id+`"`) || !strings.Contains(out.String(), `"name": "`+name+`"`) || strings.Contains(out.String(), tc.want) {
			t.Errorf("%s JSON changed device fields or text: %s", tc.language, out.String())
		}
		if tc.language != "en" {
			out.Reset()
			if code := devicesRevoke(&out, &errs, strings.NewReader("n\n"), door, []string{id}); code != 1 {
				t.Errorf("%s declined revoke exit %d: %s", tc.language, code, errs.String())
			}
			wantDecline := map[string]string{"zh-Hant": "未撤銷任何裝置", "de": "Kein Gerät wurde widerrufen"}[tc.language]
			if !strings.Contains(out.String(), id) || !strings.Contains(out.String(), wantDecline) {
				t.Errorf("%s declined revoke lost ID or negation: %q", tc.language, out.String())
			}
		}
	}
	st := contract.UpdateStatus{State: contract.UpdateStateCurrent, SourceURL: "https://example.invalid/raw-path"}
	for _, tc := range []struct{ language, want string }{{"en", "state"}, {"zh-Hant", "狀態"}, {"de", "Status"}} {
		commandLanguage = tc.language
		var out bytes.Buffer
		printUpdate(&out, st)
		if !strings.Contains(out.String(), tc.want) || !strings.Contains(out.String(), string(st.State)) || !strings.Contains(out.String(), st.SourceURL) {
			t.Errorf("%s update output changed state or URL: %q", tc.language, out.String())
		}
		if tc.language != "en" {
			out.Reset()
			var errs bytes.Buffer
			if code := applyUpdate(&out, &errs, contract.UpdateStatus{}, false, "darwin", ".", execRunner{}); code != updateExitUnknown {
				t.Errorf("%s unknown update exit = %d", tc.language, code)
			}
			wantUnknown := map[string]string{"zh-Hant": "尚未確認", "de": "unbekannt"}[tc.language]
			if !strings.Contains(errs.String(), wantUnknown) {
				t.Errorf("%s unknown update meaning lost: %q", tc.language, errs.String())
			}
		}
	}
	baseline := contract.UsageWorkSamples{Since: 100, Until: 200}
	trial := contract.UsageWorkSamples{Since: 200, Until: 300}
	var englishJSON []byte
	for _, tc := range []struct{ language, want string }{{"en", "overall:"}, {"zh-Hant", "整體："}, {"de", "Gesamt:"}} {
		commandLanguage = tc.language
		got := recomputeWorkReport(baseline, trial)
		body, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if englishJSON == nil {
			englishJSON = body
		} else if !bytes.Equal(body, englishJSON) {
			t.Errorf("%s changed usage JSON: %s", tc.language, body)
		}
		var out bytes.Buffer
		writeWorkReport(&out, got)
		if !strings.Contains(out.String(), tc.want) {
			t.Errorf("%s usage report lacks localized heading: %q", tc.language, out.String())
		}
	}
}
