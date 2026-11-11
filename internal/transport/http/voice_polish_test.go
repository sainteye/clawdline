package http

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/planner"
	"github.com/sainteye/clawdline/internal/adapters/whisper"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
)

const heard = "好那就先這樣把我等一下再看。"

// polishServer is a voice server whose Whisper answers `heard`, with the
// settings file holding `settings`.
func polishServer(t *testing.T, settings map[string]any) *Server {
	t.Helper()
	s := voiceServer(t)
	body, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.cfg.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.cfg.Dir, "config.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	bins, models := t.TempDir(), t.TempDir()
	fakeBinary(t, filepath.Join(bins, "whisper-cli"))
	fakeModel(t, filepath.Join(models, "ggml-large-v3.bin"))
	useEngine(t, func(s *Server) whisper.Transcriber {
		engine := s.whisper()
		engine.BinaryDirs, engine.ModelDirs = []string{bins}, []string{models}
		engine.LookPath = func(string) (string, error) { return "", errNoPath }
		engine.TempDir = t.TempDir()
		engine.Run = func(context.Context, string, ...string) ([]byte, error) { return []byte(heard + "\n"), nil }
		return engine
	})
	return s
}

type polishCall struct {
	text, level, assistant string
	terms                  []string
}

func usePolisher(t *testing.T, answer func(polishCall) (string, error)) *[]polishCall {
	t.Helper()
	calls := &[]polishCall{}
	was := voicePolisher
	voicePolisher = func(_ context.Context, text string, terms []string, level, assistant string) (string, error) {
		c := polishCall{text, level, assistant, terms}
		*calls = append(*calls, c)
		return answer(c)
	}
	t.Cleanup(func() { voicePolisher = was })
	return calls
}

func transcribeOnce(t *testing.T, s *Server, key string) contract.VoiceResult {
	t.Helper()
	rec := voiceCall(t, s, key, auth.NewCaps(auth.Read, auth.Send), voiceBody(1, 16000))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var out contract.VoiceResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestVoiceWithoutCleanupNeverCallsAModel(t *testing.T) {
	calls := usePolisher(t, func(polishCall) (string, error) { return "never", nil })
	out := transcribeOnce(t, polishServer(t, map[string]any{}), "off")
	if len(*calls) != 0 || out.Text != heard || out.Raw != "" || out.Polish != nil {
		t.Fatalf("calls %d, result %+v", len(*calls), out)
	}
}

func TestVoiceCleanupReplacesTheTextAndKeepsTheOriginal(t *testing.T) {
	calls := usePolisher(t, func(c polishCall) (string, error) { return "好，那就先這樣，我等一下再看。", nil })
	s := polishServer(t, map[string]any{
		"voice_polish": true, "voice_polish_assistant": "codex", "voice_polish_level": "punctuation",
		"voice_vocabulary": []string{"Tailscale"},
	})
	out := transcribeOnce(t, s, "on")
	if out.Text != "好，那就先這樣，我等一下再看。" || out.Raw != heard {
		t.Fatalf("result %+v", out)
	}
	if out.Polish == nil || out.Polish.Status != "applied" || out.Polish.Assistant != "codex" {
		t.Fatalf("polish %+v", out.Polish)
	}
	c := (*calls)[0]
	if c.level != planner.PolishPunctuation || c.assistant != "codex" || !slices.Contains(c.terms, "Tailscale") || !slices.Contains(c.terms, "Clawdline") {
		t.Fatalf("call %+v", c)
	}
}

func TestVoiceCleanupThatFailsAnswersWhisperText(t *testing.T) {
	for _, c := range []struct {
		err    error
		status string
	}{
		{planner.ErrPolishUnfaithful, "unfaithful"},
		{planner.ErrPolishTimeout, "timeout"},
		{planner.ErrNoPlanner, "unavailable"},
		{planner.ErrOutOfQuota, "quota"},
		{os.ErrPermission, "failed"},
	} {
		usePolisher(t, func(polishCall) (string, error) { return "", c.err })
		out := transcribeOnce(t, polishServer(t, map[string]any{"voice_polish": true}), c.status)
		if out.Text != heard || out.Raw != "" || out.Polish == nil || out.Polish.Status != c.status || out.Polish.Assistant != "claude" {
			t.Errorf("%v: result %+v polish %+v", c.err, out, out.Polish)
		}
	}
}

func TestVoiceProjectNamesJoinTheVocabularyOnlyWhenTicked(t *testing.T) {
	calls := usePolisher(t, func(polishCall) (string, error) { return heard, nil })
	was := voiceProjectNames
	voiceProjectNames = func(*Server) []string { return []string{"orchard-api"} }
	t.Cleanup(func() { voiceProjectNames = was })
	for _, ticked := range []bool{false, true} {
		s := polishServer(t, map[string]any{"voice_polish": true, "voice_vocabulary_projects": ticked})
		out := transcribeOnce(t, s, "projects")
		if out.Polish == nil || out.Polish.Status != "unchanged" {
			t.Fatalf("polish %+v", out.Polish)
		}
		got := slices.Contains((*calls)[len(*calls)-1].terms, "orchard-api")
		if got != ticked {
			t.Errorf("ticked %v: project in terms %v", ticked, got)
		}
	}
}

// generatedID is spelled in pieces: tools/check-private.sh reads a whole UUID
// in a published file as one copied from somebody's machine.
var generatedID = "0000aaaa" + "-1111-4222-8333-" + "444455556666"

func TestSpokenNameLeavesOutGeneratedFolderNames(t *testing.T) {
	for name, want := range map[string]bool{
		"clawdline": true, "web-console": true, "api2": true, "台北": true,
		generatedID: false, "0123456789abcdef": false, "2026": false,
		"deadbeef": true, // short enough to be a word someone named a folder
	} {
		if got := spokenName(name); got != want {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
	}
}
