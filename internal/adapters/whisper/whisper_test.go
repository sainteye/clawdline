package whisper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A machine with neither half, one with only the binary, and one with both:
// the three answers this package exists to tell apart. Every one of them is
// asked with search paths of the test's own, so nothing that is actually
// installed on the machine running this is touched or consulted.
func TestReadyNamesWhichHalfIsMissing(t *testing.T) {
	empty := t.TempDir()
	bins, models := t.TempDir(), t.TempDir()
	fake(t, filepath.Join(bins, "whisper-cli"))

	nowhere := Transcriber{BinaryDirs: []string{empty}, ModelDirs: []string{empty}, LookPath: never}
	if _, _, err := nowhere.Ready(); !errors.Is(err, ErrNoBinary) {
		t.Fatalf("no binary anywhere: %v", err)
	}

	half := Transcriber{BinaryDirs: []string{bins}, ModelDirs: []string{models}, LookPath: never}
	if _, _, err := half.Ready(); !errors.Is(err, ErrNoModel) {
		t.Fatalf("binary and no model: %v", err)
	}

	// `brew install whisper-cpp` leaves this file beside the binary. It is a
	// fixture and not a model, and finding it would turn "no model" into a
	// transcript of nonsense.
	write(t, filepath.Join(models, "for-tests-ggml-tiny.bin"), 4096)
	if _, _, err := half.Ready(); !errors.Is(err, ErrNoModel) {
		t.Fatalf("the test fixture was taken for a model: %v", err)
	}

	write(t, filepath.Join(models, "ggml-small.bin"), 2048)
	write(t, filepath.Join(models, "ggml-large-v3.bin"), 8192)
	write(t, filepath.Join(models, "notes.txt"), 9999)
	binary, model, err := half.Ready()
	if err != nil {
		t.Fatalf("both halves present: %v", err)
	}
	if binary != filepath.Join(bins, "whisper-cli") {
		t.Fatalf("binary = %s", binary)
	}
	// Largest, because if somebody downloaded two the big one is the one they
	// meant to use.
	if filepath.Base(model) != "ggml-large-v3.bin" {
		t.Fatalf("model = %s", model)
	}
}

// A configured path is consulted before the search, and a configured path that
// is not there does not stop the search finding the ordinary one.
func TestConfiguredPathsComeFirstAndDoNotBlock(t *testing.T) {
	bins, models := t.TempDir(), t.TempDir()
	fake(t, filepath.Join(bins, "whisper-cli"))
	write(t, filepath.Join(models, "ggml-small.bin"), 2048)
	mine := filepath.Join(t.TempDir(), "my-whisper")
	fake(t, mine)

	t1 := Transcriber{Binary: mine, BinaryDirs: []string{bins}, ModelDirs: []string{models}, LookPath: never}
	if got, _, err := t1.Ready(); err != nil || got != mine {
		t.Fatalf("configured binary = %s (%v)", got, err)
	}
	t2 := Transcriber{Binary: filepath.Join(t.TempDir(), "gone"), BinaryDirs: []string{bins},
		ModelDirs: []string{models}, LookPath: never}
	if got, _, err := t2.Ready(); err != nil || got != filepath.Join(bins, "whisper-cli") {
		t.Fatalf("fell back to = %s (%v)", got, err)
	}
}

// The whole run, with the process replaced: the arguments whisper is given,
// the WAV that is written for it, and the temporary file afterwards.
func TestTranscribeWritesAWAVRunsOnceAndCleansUp(t *testing.T) {
	bins, models, temp := t.TempDir(), t.TempDir(), t.TempDir()
	fake(t, filepath.Join(bins, "whisper-cli"))
	write(t, filepath.Join(models, "ggml-large-v3.bin"), 2048)

	var saw []string
	var wav []byte
	engine := Transcriber{
		Language: "zh-TW", TempDir: temp,
		BinaryDirs: []string{bins}, ModelDirs: []string{models}, LookPath: never,
		Run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			saw = args
			for i, a := range args {
				if a == "-f" && i+1 < len(args) {
					wav, _ = os.ReadFile(args[i+1])
				}
			}
			return []byte("[00:00.000 --> 00:02.000]\n 那個 webhook 的 retry\n"), nil
		},
	}
	text, err := engine.Transcribe(context.Background(), speech(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if text != "那個 webhook 的 retry" {
		t.Fatalf("text = %q", text)
	}
	joined := strings.Join(saw, " ")
	for _, want := range []string{"-nt", "-np", "-sns", "-nth 0.8", "-l zh", "--prompt"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %v", want, saw)
		}
	}
	// The script seed and nothing else. A prompt that is mostly a word list
	// costs the transcript its punctuation, which is the measurement the
	// vocabulary repair exists because of.
	if !strings.Contains(joined, Seeds["zh-Hant"]) {
		t.Fatalf("no Traditional seed: %v", saw)
	}
	for _, term := range AlwaysExpected {
		if strings.Contains(joined, "--prompt "+term) {
			t.Fatalf("vocabulary reached the prompt: %v", saw)
		}
	}
	if len(wav) < 44 || string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		t.Fatalf("no WAV was written (%d bytes)", len(wav))
	}
	// Nothing is left behind. A directory slowly filling with somebody's
	// speech is not a thing this is allowed to leave.
	left, _ := os.ReadDir(temp)
	if len(left) != 0 {
		t.Fatalf("temporary files left: %d", len(left))
	}
}

// A run that overruns its own clock is a named failure and not an empty
// transcript. "The model fell over" and "the room was quiet" are different
// facts, and only one of them is worth trying again.
func TestTranscribeNamesATimeout(t *testing.T) {
	bins, models := t.TempDir(), t.TempDir()
	fake(t, filepath.Join(bins, "whisper-cli"))
	write(t, filepath.Join(models, "ggml-large-v3.bin"), 2048)
	engine := Transcriber{
		Timeout: 20 * time.Millisecond, TempDir: t.TempDir(),
		BinaryDirs: []string{bins}, ModelDirs: []string{models}, LookPath: never,
		Run: func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	if _, err := engine.Transcribe(context.Background(), speech(time.Second)); !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v", err)
	}
}

// Under a quarter of a second is a button pressed by accident, and it is
// refused before a 600MB model is asked to read it.
func TestTooShortIsRefusedWithoutRunningAnything(t *testing.T) {
	engine := Transcriber{
		BinaryDirs: []string{t.TempDir()}, ModelDirs: []string{t.TempDir()}, LookPath: never,
		Run: func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("a tap reached whisper")
			return nil, nil
		},
	}
	if _, err := engine.Transcribe(context.Background(), speech(100*time.Millisecond)); !errors.Is(err, ErrTooShort) {
		t.Fatalf("err = %v", err)
	}
}

func TestWAVHeaderSaysWhatTheSamplesAre(t *testing.T) {
	out := WAV(make([]byte, 320), Rate)
	if len(out) != 364 {
		t.Fatalf("length = %d", len(out))
	}
	u32 := func(at int) uint32 {
		return uint32(out[at]) | uint32(out[at+1])<<8 | uint32(out[at+2])<<16 | uint32(out[at+3])<<24
	}
	u16 := func(at int) uint16 { return uint16(out[at]) | uint16(out[at+1])<<8 }
	if string(out[:4]) != "RIFF" || string(out[8:12]) != "WAVE" || string(out[12:16]) != "fmt " {
		t.Fatalf("header = %q", out[:16])
	}
	if u32(4) != 356 || u32(16) != 16 || u16(20) != 1 || u16(22) != 1 {
		t.Fatalf("sizes wrong: %d %d %d %d", u32(4), u32(16), u16(20), u16(22))
	}
	if u32(24) != Rate || u32(28) != Rate*2 || u16(32) != 2 || u16(34) != 16 {
		t.Fatalf("format wrong: %d %d %d %d", u32(24), u32(28), u16(32), u16(34))
	}
	if string(out[36:40]) != "data" || u32(40) != 320 {
		t.Fatalf("data chunk = %q %d", out[36:40], u32(40))
	}
}

// The threshold is relative to the room and not to the loudest moment. A
// sentence that falls away as it ends is a thing people do; cutting the last
// few words off it produces a correct transcription of slightly less than was
// said, which is far harder to notice than a wrong one.
func TestTrimSilenceKeepsTheQuietEndOfASentence(t *testing.T) {
	clip := []byte{}
	clip = append(clip, tone(time.Second, 40)...)   // room
	clip = append(clip, tone(time.Second, 9000)...) // the sentence
	clip = append(clip, tone(time.Second, 900)...)  // its quiet tail
	clip = append(clip, tone(time.Second, 40)...)   // room again
	cut := TrimSilence(clip, Rate, 2.5)
	if len(cut) >= len(clip) {
		t.Fatalf("nothing was cut: %d of %d", len(cut), len(clip))
	}
	// A quarter of the peak would have stopped in front of the tail; this is
	// the assertion that says it did not.
	if len(cut) < int(float64(Rate)*2*2*0.9) {
		t.Fatalf("the tail was cut: kept %.2fs", float64(len(cut))/(Rate*2))
	}
	// Silence all the way through has nothing this can honestly keep.
	if got := TrimSilence(tone(2*time.Second, 0), Rate, 2.5); len(got) != 0 {
		t.Fatalf("silence kept %d bytes", len(got))
	}
}

func TestLooksLikeLoopKnowsAGrooveFromASentence(t *testing.T) {
	grooves := []string{"和音，和音，和音，和音，", "好,好,好。", "謝謝大家謝謝大家謝謝大家謝謝大家"}
	for _, said := range grooves {
		if !LooksLikeLoop(said) {
			t.Fatalf("%q was taken for a sentence", said)
		}
	}
	sentences := []string{"那個 webhook 的 retry 改成 exponential backoff", "好的，我知道了。", "", "yes"}
	for _, said := range sentences {
		if LooksLikeLoop(said) {
			t.Fatalf("%q was taken for a groove", said)
		}
	}
}

func TestTidyPutsTheSpacesAndThePunctuationBack(t *testing.T) {
	cases := [][2]string{
		{"那個webhook的retry", "那個 webhook 的 retry"},
		{"改成 exponential backoff", "改成 exponential backoff"},
		{"先這樣,等一下再說", "先這樣，等一下再說"},
		{"Okay, got it.", "Okay, got it."},
		{"用 curl:再說", "用 curl：再說"},
	}
	for _, c := range cases {
		if got := Tidy(c[0]); got != c[1] {
			t.Fatalf("Tidy(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}

// Calibrated against the two failures that matter, in both directions.
func TestApplyVocabularyIsTolerantAndNotGullible(t *testing.T) {
	cases := [][2]string{
		{"cloudline is down", "Clawdline is down"},
		{"ask cloud code about it", "ask Claude Code about it"},
		{"把 cloudline 重開", "把 Clawdline 重開"},
		// One edit from "Clawd" and a word in its own right.
		{"claw at it", "claw at it"},
		{"整個 Clawdline 都很好", "整個 Clawdline 都很好"},
	}
	for _, c := range cases {
		if got := ApplyVocabulary(c[0], AlwaysExpected); got != c[1] {
			t.Fatalf("ApplyVocabulary(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}

// A defect of the original's, inherited on purpose and written down so that
// nobody has to rediscover it.
//
// A term that is already spelled right is skipped rather than claimed — "an
// exact match differing only in case is usually the writer's own
// capitalisation" — and skipping it leaves its word free for the next size to
// take. So "Clawdline is" is eleven letters, two edits from "clawdline", and
// inside the two this term's length buys: the pair is replaced by the term and
// the "is" disappears. `Whisper.applyVocabulary` does the same thing with the
// same numbers, and this project replicates the Swift app rather than
// improving on it, so the fix belongs over there first.
func TestApplyVocabularyEatsTheNextWordAfterAnExactMatch(t *testing.T) {
	if got := ApplyVocabulary("Clawdline is fine", AlwaysExpected); got != "Clawdline fine" {
		t.Fatalf("got %q; the inherited behaviour changed, which is worth a look", got)
	}
}

func TestToTraditionalConvertsCharactersAndNotWording(t *testing.T) {
	if got := ToTraditional("网络后台"); got != "網絡後台" {
		t.Fatalf("got %q", got)
	}
	// Already Traditional, and English in the middle of a sentence: both pass
	// through untouched, because no Traditional character is a key in the
	// table.
	if got := ToTraditional("那個 webhook 的 retry 網路"); got != "那個 webhook 的 retry 網路" {
		t.Fatalf("got %q", got)
	}
	if got := ToTraditional("nothing to do here"); got != "nothing to do here" {
		t.Fatalf("got %q", got)
	}
}

func TestLanguagePicksTheCodeAndTheScript(t *testing.T) {
	cases := []struct{ tag, code, seed string }{
		{"zh-TW", "zh", Seeds["zh-Hant"]},
		{"zh-HK", "zh", Seeds["zh-Hant"]},
		{"zh-CN", "zh", Seeds["zh-Hans"]},
		{"en-GB", "en", Seeds["en"]},
		{"auto", "auto", ""},
		{"", "auto", ""},
		{"xx", "xx", ""},
	}
	for _, c := range cases {
		code, seed := Language(c.tag)
		if code != c.code || seed != c.seed {
			t.Fatalf("Language(%q) = %q/%q", c.tag, code, seed)
		}
	}
}

// ---------- fixtures ----------

func never(string) (string, error) { return "", errors.New("not on PATH") }

func fake(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

// speech is a clip loud enough to survive the trim, so a test about something
// else is not silently testing the trim.
func speech(d time.Duration) []byte { return tone(d, 9000) }

// tone is `d` of a square wave at `amplitude`, as little-endian Int16 mono.
func tone(d time.Duration, amplitude int) []byte {
	count := int(float64(Rate) * d.Seconds())
	out := make([]byte, count*2)
	for i := 0; i < count; i++ {
		v := amplitude
		if (i/40)%2 == 0 {
			v = -amplitude
		}
		out[i*2] = byte(uint16(int16(v)) & 0xFF)
		out[i*2+1] = byte(uint16(int16(v)) >> 8)
	}
	return out
}
