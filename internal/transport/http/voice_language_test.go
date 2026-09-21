package http

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline-go/internal/adapters/whisper"
	"github.com/sainteye/clawdline-go/internal/contract"
)

// A machine this test decides, rather than the one running it. This one is set
// to zh-TW, which is exactly where a rule that always says Traditional would
// pass; so every case names its machine, and two of them are machines that
// are not this one.
func asMachine(t *testing.T, m whisper.Machine) {
	t.Helper()
	was := voiceMachine
	voiceMachine = m
	t.Cleanup(func() { voiceMachine = was })
}

func macSaying(langs, locale string) whisper.Machine {
	return whisper.Machine{GOOS: "darwin", Getenv: func(string) string { return "" },
		Defaults: func(_ context.Context, key string) (string, error) {
			switch {
			case key == "AppleLanguages" && langs != "":
				return langs, nil
			case key == "AppleLocale" && locale != "":
				return locale, nil
			}
			return "", errors.New("The domain/default pair does not exist")
		}}
}

func linuxWith(env map[string]string) whisper.Machine {
	return whisper.Machine{GOOS: "linux", Getenv: func(k string) string { return env[k] }}
}

func writeSettings(t *testing.T, s *Server, body string) {
	t.Helper()
	if err := os.MkdirAll(s.cfg.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.cfg.Dir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// transcribeAs runs this daemon's own transcriber, as the route builds it, with
// only the process replaced: whisper answers `said`, and the arguments it was
// given come back with the text.
func transcribeAs(t *testing.T, s *Server, said string) (string, string) {
	t.Helper()
	engine := s.whisper()
	bins, models := t.TempDir(), t.TempDir()
	fakeBinary(t, filepath.Join(bins, "whisper-cli"))
	fakeModel(t, filepath.Join(models, "ggml-large-v3.bin"))
	engine.BinaryDirs, engine.ModelDirs, engine.TempDir = []string{bins}, []string{models}, t.TempDir()
	engine.LookPath = func(string) (string, error) { return "", errNoPath }
	var args []string
	engine.Run = func(_ context.Context, _ string, a ...string) ([]byte, error) {
		args = a
		return []byte(said + "\n"), nil
	}
	var body struct{ Audio string }
	_ = json.Unmarshal([]byte(voiceBody(1, 16000)), &body)
	samples, _ := base64.StdEncoding.DecodeString(body.Audio)
	text, err := engine.Transcribe(context.Background(), samples)
	if err != nil {
		t.Fatal(err)
	}
	return text, strings.Join(args, " ")
}

const (
	saidHans = "我们现在开始说话。"
	saidHant = "我們現在開始說話。"
)

// **`auto` follows Clawdline, then the machine, then the catalog this daemon
// ships — and Simplified comes back only when one of them asked for it.**
//
// Before this, a settings file with no `voice_language` gave whisper `-l auto`
// and no prompt, and whisper wrote a Traditional person's Mandarin in
// Simplified: artifacts/red-before-fix.txt is that run, on this code's
// predecessor, on a machine set to zh-TW.
func TestVoiceAutoFollowsClawdlineThenTheMachine(t *testing.T) {
	cases := []struct {
		name     string
		settings string
		machine  whisper.Machine
		said     string
		want     string
		lang     string // what -l was given
		seed     string // the prompt, or "" for none
		source   string // who decided -l
		script   string
	}{
		// The three the brief asks for.
		{"machine zh-TW", `{}`, macSaying("(\n    \"zh-Hant-TW\"\n)\n", "zh_TW"),
			saidHans, saidHant, "-l zh", whisper.Seeds["zh-Hant"], whisper.FromAppleLangs, "Hant"},
		{"machine zh-CN", `{}`, linuxWith(map[string]string{"LANG": "zh_CN.UTF-8"}),
			saidHans, saidHans, "-l zh", whisper.Seeds["zh-Hans"], "LANG", "Hans"},
		// Nothing readable: not whisper's habit, but the language every page
		// this daemon serves is already written in — and it says so.
		{"machine says nothing", `{}`, macSaying("", ""),
			saidHans, saidHant, "-l zh", whisper.Seeds["zh-Hant"], whisper.FromCatalog, "Hant"},
		{"linux says C", `{}`, linuxWith(map[string]string{"LANG": "C.UTF-8", "LC_ALL": "POSIX"}),
			saidHans, saidHant, "-l zh", whisper.Seeds["zh-Hant"], whisper.FromCatalog, "Hant"},

		// Clawdline's own language comes before the machine's, in both
		// directions.
		{"Clawdline zh-Hans on a zh-TW machine", `{"language":"zh-Hans"}`, macSaying("(\n    \"zh-Hant-TW\"\n)\n", ""),
			saidHans, saidHans, "-l zh", whisper.Seeds["zh-Hans"], whisper.FromLanguage, "Hans"},
		{"Clawdline zh-Hant on a zh-CN machine", `{"language":"zh-Hant"}`, linuxWith(map[string]string{"LANG": "zh_CN.UTF-8"}),
			saidHans, saidHant, "-l zh", whisper.Seeds["zh-Hant"], whisper.FromLanguage, "Hant"},
		// `language: auto` is a question, and is passed on to the machine.
		{"Clawdline auto on a zh-CN machine", `{"language":"auto"}`, linuxWith(map[string]string{"LANG": "zh_CN.UTF-8"}),
			saidHans, saidHans, "-l zh", whisper.Seeds["zh-Hans"], "LANG", "Hans"},

		// An explicit voice language outranks all of it.
		{"voice_language zh-CN", `{"voice_language":"zh-CN","language":"zh-Hant"}`, macSaying("(\n    \"zh-Hant-TW\"\n)\n", ""),
			saidHans, saidHans, "-l zh", whisper.Seeds["zh-Hans"], whisper.FromVoiceLanguage, "Hans"},
		{"voice_language zh-TW", `{"voice_language":"zh-TW"}`, linuxWith(map[string]string{"LANG": "zh_CN.UTF-8"}),
			saidHans, saidHant, "-l zh", whisper.Seeds["zh-Hant"], whisper.FromVoiceLanguage, "Hant"},

		// A Clawdline in English does not force English on whisper — that would
		// turn somebody's Mandarin into a translation — but the Chinese that
		// comes back is still written the way the machine's list says.
		{"Clawdline en, machine Traditional second", `{"language":"en"}`,
			macSaying("(\n    \"en-US\",\n    \"zh-Hant-TW\"\n)\n", ""),
			saidHans, saidHant, "-l auto", "", whisper.FromLanguage, "Hant"},
		{"Clawdline en, Japanese heard", `{"language":"en"}`,
			macSaying("(\n    \"en-US\",\n    \"zh-Hant-TW\"\n)\n", ""),
			"学校に行きます。", "学校に行きます。", "-l auto", "", whisper.FromLanguage, "Hant"},
		{"Clawdline en, English heard", `{"language":"en"}`, macSaying("", ""),
			"Push the branch.", "Push the branch.", "-l auto", "", whisper.FromLanguage, "Hant"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := voiceServer(t)
			writeSettings(t, s, c.settings)
			asMachine(t, c.machine)

			text, args := transcribeAs(t, s, c.said)
			if text != c.want {
				t.Fatalf("text = %q, want %q", text, c.want)
			}
			if !strings.Contains(args, c.lang+" ") {
				t.Fatalf("whisper was not given %q: %s", c.lang, args)
			}
			if c.seed == "" && strings.Contains(args, "--prompt") {
				t.Fatalf("a prompt reached a run left to detection: %s", args)
			}
			if c.seed != "" && !strings.Contains(args, "--prompt "+c.seed) {
				t.Fatalf("prompt is not %q: %s", c.seed, args)
			}

			// And the settings window is told the same thing the run did.
			rec := httptest.NewRecorder()
			s.voiceLanguageRoute(rec, httptest.NewRequest(http.MethodGet, "/v1/voice/language", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /v1/voice/language: %d %s", rec.Code, rec.Body)
			}
			var got contract.VoiceLanguage
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if "-l "+got.Code != c.lang || got.Source != c.source || got.Script != c.script {
				t.Fatalf("GET says %+v; the run was %s from %s, script %s", got, c.lang, c.source, c.script)
			}
		})
	}
}

// The settings window can write the key, and a value that could not be a
// language is refused by name rather than stored for whisper to choke on.
func TestVoiceLanguageIsASettableKey(t *testing.T) {
	s := voiceServer(t)
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/settings", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.settingsRoute(rec, req)
		return rec
	}
	for _, ok := range []string{"auto", "zh-Hant", "zh-Hans", "zh_TW", "en"} {
		if rec := post(`{"voice_language":"` + ok + `"}`); rec.Code != http.StatusOK {
			t.Fatalf("%s refused: %d %s", ok, rec.Code, rec.Body)
		}
	}
	var snap contract.SettingsSnapshot
	rec := post(`{"voice_language":"zh-Hant"}`)
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil || snap.VoiceLanguage == nil || *snap.VoiceLanguage != "zh-Hant" {
		t.Fatalf("snapshot = %s (%v)", rec.Body, err)
	}
	for _, bad := range []string{`""`, `"../zh"`, `"zh Hant"`, `"1zh"`, `7`} {
		rec := post(`{"voice_language":` + bad + `}`)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_voice_language") {
			t.Fatalf("%s: %d %s", bad, rec.Code, rec.Body)
		}
	}
}
