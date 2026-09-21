// Package whisper turns a recording into words by running whisper.cpp's
// `whisper-cli`, which is the Swift app's `Whisper.swift` and nothing more
// ambitious: this daemon owns no model and links no inference library.
//
// **Optional on purpose, and it says which half is missing.** Nothing here
// runs unless a binary and a model are both present, and `brew install
// whisper-cpp` leaves a machine with the first and not the second — which is
// the commonest state and the reason `Status` tells the two apart. Told only
// "off", a person goes and checks the thing they already did.
//
// **The samples are the only shape this takes**: little-endian 16-bit mono at
// 16 kHz. Not a preference — whisper.cpp takes 16 kHz and nothing else, and
// the WAV header is written around these bytes here rather than shelling out
// to a converter this machine may not have. The browser resamples before it
// uploads for the same reason (`legacy/voice-bridge.ts`).
//
// **One run at a time, per process.** Two whispers at once on one machine are
// slower than two in a row, so the lock is not a resource limit, it is the
// arrangement that makes the second one finish sooner. The route's queue depth
// is what decides how long a line is worth standing in.
package whisper

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// The refusals this package has, as values a caller branches on. The sentences
// belong to whoever is answering a request; these say which of the four
// happened.
var (
	// ErrNoBinary is a machine with no whisper-cli on it.
	ErrNoBinary = errors.New("no whisper-cli on this machine")
	// ErrNoModel is whisper-cli installed with no ggml model to read with.
	ErrNoModel = errors.New("whisper-cli is here and has no model")
	// ErrTimeout is a run that was still going when its time ran out.
	ErrTimeout = errors.New("whisper-cli did not finish in time")
	// ErrTooShort is a recording under the floor: a tap, not a sentence.
	ErrTooShort = errors.New("that recording is too short to read")
	// ErrFailed is whisper-cli exiting non-zero, or writing nothing readable.
	ErrFailed = errors.New("whisper-cli could not read that recording")
)

// Rate is the only sample rate this takes, and the shape that comes with it.
// One rate is what makes the browser's path and a native recorder's path the
// same path.
const Rate = 16000

// Floor is a quarter of a second: under it is a button pressed by accident.
// The same floor the Swift app keeps, in both of the places that check it.
const Floor = 250 * time.Millisecond

// DefaultTimeout bounds one run. `Whisper.swift` uses sixty seconds for every
// process it starts, and the first transcription after a reboot spends twelve
// of its seconds reading a 600MB model off disk.
const DefaultTimeout = 60 * time.Second

// AlwaysExpected are `Voice.alwaysExpected`: names no model has in its
// vocabulary and this app is certain to be told. "Clawdline" is not a word in
// any language, and "Claude Code" is two ordinary English words arranged in a
// way no corpus has seen, so both come back as something that merely sounds
// right.
var AlwaysExpected = []string{
	"Clawdline", "Claude", "Claude Code", "Clawd", "Clawdfather",
	"Commit", "Session", "Agent",
}

// Seeds are `Whisper.seeds`, one punctuated sentence per language.
//
// Two separate jobs, and `-l zh` only does the first: it picks the language
// and not the script, and whisper's Chinese comes out Simplified by default.
// The initial prompt does the second — and because the prompt is a writing
// sample rather than a list, a sample with commas and full stops in it
// produces them. It says nothing in particular on purpose: what is being shown
// is the punctuation, not the subject.
var Seeds = map[string]string{
	"zh-Hant": "好的，我知道了。那就先這樣，等一下再說。",
	"zh-Hans": "好的，我知道了。那就先这样，等一下再说。",
	"en":      "Okay, got it. Let's leave it there for now, and pick it up later.",
	"ja":      "はい、わかりました。ひとまずこれで、あとでまた話しましょう。",
	"ko":      "네, 알겠습니다. 일단 여기까지 하고, 나중에 다시 이야기하죠.",
	"es":      "De acuerdo, entendido. Lo dejamos aquí por ahora y seguimos luego.",
	"pt":      "Certo, entendi. Vamos parar por aqui e continuamos depois.",
	"fr":      "D'accord, c'est noté. On en reste là pour l'instant, on reprendra plus tard.",
	"de":      "Alles klar, verstanden. Wir lassen es vorerst so und machen später weiter.",
	"it":      "Va bene, capito. Per ora ci fermiamo qui e riprendiamo dopo.",
	"ru":      "Хорошо, понял. Пока остановимся на этом, продолжим позже.",
}

// one whisper per process, across every route that reaches this package.
var running sync.Mutex

// Transcriber is one machine's whisper, as configured.
//
// Every field has a working zero value, and the search fields exist so that
// "not installed" and "installed with no model" can be exercised by pointing
// the search somewhere empty rather than by moving what is installed.
type Transcriber struct {
	// Binary is a configured path to whisper-cli. Empty means search.
	Binary string
	// Model is a configured path to a ggml model. Empty means search.
	Model string
	// Language is a BCP-47 tag like "zh-TW", or "auto".
	Language string
	// Follow is who `auto` asks, in order (locale.go): Clawdline's own
	// language, this machine's, then the catalog this daemon ships. Empty
	// leaves `auto` to whisper, which writes Chinese in Simplified.
	Follow []Answer
	// Vocabulary is repaired in the text afterwards, never put in the prompt.
	// See ApplyVocabulary for the measurement behind that.
	Vocabulary []string
	// Timeout bounds one run; zero means DefaultTimeout.
	Timeout time.Duration
	// TempDir is where the WAV is written; empty means the system's.
	TempDir string

	// BinaryDirs and ModelDirs replace the search below. Empty means the
	// ordinary places for this operating system.
	BinaryDirs []string
	ModelDirs  []string
	// LookPath is exec.LookPath, replaced in tests. It is consulted only
	// after the directories, as the Swift app consults `which` only after
	// Homebrew's two.
	LookPath func(string) (string, error)
	// Run executes the command and returns its standard output. Replaced in
	// tests so a run can be exercised without a model.
	Run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Ready is the binary and model this machine would use, or the refusal that
// stops it. The model is returned by its file name, which is what a person
// recognises; the path stays inside.
func (t Transcriber) Ready() (binary, model string, err error) {
	binary, ok := t.findBinary()
	if !ok {
		return "", "", ErrNoBinary
	}
	model, ok = t.findModel()
	if !ok {
		return binary, "", ErrNoModel
	}
	return binary, model, nil
}

// binaryNames are what whisper.cpp's command-line tool has been called.
// `main` is the older spelling, which is why more than one name is tried.
func binaryNames() []string {
	names := []string{"whisper-cli", "whisper-cpp", "main"}
	if runtime.GOOS == "windows" {
		for i, n := range names {
			names[i] = n + ".exe"
		}
	}
	return names
}

// binaryDirs is where each operating system puts it.
//
// macOS is Homebrew on both architectures, which is what the Swift app looks
// at. Linux is the distribution's own two plus whisper.cpp's in-tree build
// directory, because that is how most people on Linux have it — built rather
// than packaged. Windows is where an installer and a scoop/winget shim land.
func (t Transcriber) binaryDirs() []string {
	if len(t.BinaryDirs) > 0 {
		return t.BinaryDirs
	}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return []string{"/opt/homebrew/bin", "/usr/local/bin"}
	case "windows":
		dirs := []string{}
		for _, key := range []string{"LOCALAPPDATA", "ProgramFiles", "ProgramFiles(x86)"} {
			if base := os.Getenv(key); base != "" {
				dirs = append(dirs, filepath.Join(base, "whisper.cpp"),
					filepath.Join(base, "whisper.cpp", "bin"))
			}
		}
		return dirs
	default:
		dirs := []string{"/usr/local/bin", "/usr/bin", "/opt/whisper.cpp/build/bin"}
		if home != "" {
			dirs = append(dirs, filepath.Join(home, ".local", "bin"),
				filepath.Join(home, "whisper.cpp", "build", "bin"))
		}
		return dirs
	}
}

func (t Transcriber) findBinary() (string, bool) {
	if t.Binary != "" && executable(t.Binary) {
		return t.Binary, true
	}
	for _, dir := range t.binaryDirs() {
		for _, name := range binaryNames() {
			p := filepath.Join(dir, name)
			if executable(p) {
				return p, true
			}
		}
	}
	look := t.LookPath
	if look == nil {
		look = exec.LookPath
	}
	for _, name := range binaryNames() {
		if p, err := look(name); err == nil && p != "" {
			return p, true
		}
	}
	return "", false
}

// executable reports whether this path is a file this process could run.
//
// On Windows the permission bits say nothing, so the question there is only
// whether the file is there: a `.exe` that exists is one Windows will run.
func executable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode().Perm()&0o111 != 0
}

// modelDirs is where a `ggml-*.bin` is kept. `~/.cache/whisper` is where
// whisper.cpp's own download script puts one on every platform, so it is first
// everywhere.
func (t Transcriber) modelDirs() []string {
	if len(t.ModelDirs) > 0 {
		return t.ModelDirs
	}
	home, _ := os.UserHomeDir()
	dirs := []string{}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".cache", "whisper"))
	}
	switch runtime.GOOS {
	case "darwin":
		if home != "" {
			// The Swift app's own place, read and never written: a machine
			// that already downloaded a model for the bar should not have to
			// download it twice.
			dirs = append(dirs, filepath.Join(home, "Library", "Application Support", "Clawdline", "models"))
		}
	case "windows":
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			dirs = append(dirs, filepath.Join(base, "whisper"), filepath.Join(base, "whisper.cpp", "models"))
		}
	default:
		if home != "" {
			dirs = append(dirs, filepath.Join(home, ".local", "share", "whisper"))
		}
		dirs = append(dirs, "/usr/share/whisper", "/usr/local/share/whisper", "/opt/whisper.cpp/models")
	}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, "models"))
	}
	return dirs
}

// findModel is the configured file, else the largest `ggml-*.bin` lying in the
// usual places — largest because if somebody downloaded two, the big one is
// the one they meant to use.
//
// `for-tests-ggml-tiny.bin` is skipped by name. Homebrew ships it beside the
// binary and it is a fixture rather than a model: finding it would turn "no
// model" into a transcript of nonsense, which is the worse of the two answers.
func (t Transcriber) findModel() (string, bool) {
	if t.Model != "" && readable(t.Model) {
		return t.Model, true
	}
	best, size := "", int64(-1)
	for _, dir := range t.modelDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		// Sorted, so two models of the same size are chosen the same way on
		// every machine rather than in whatever order the directory holds.
		sort.Strings(names)
		for _, name := range names {
			if !strings.HasPrefix(name, "ggml-") || !strings.HasSuffix(name, ".bin") ||
				strings.Contains(name, "for-tests") {
				continue
			}
			p := filepath.Join(dir, name)
			info, err := os.Stat(p)
			if err != nil || info.IsDir() {
				continue
			}
			if info.Size() > size {
				best, size = p, info.Size()
			}
		}
	}
	return best, best != ""
}

func readable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	return err == nil && !info.IsDir()
}

// Language is `Whisper.language(for:)`: the code whisper is given, and the
// sentence it is seeded with.
func Language(tag string) (code, seed string) {
	lower := strings.ToLower(strings.TrimSpace(tag))
	if lower == "" || lower == "auto" {
		return "auto", ""
	}
	if strings.HasPrefix(lower, "zh") {
		// Which script, not which country: the seed is the only lever on
		// that, short of converting afterwards.
		return "zh", Seeds[map[bool]string{true: "zh-Hant", false: "zh-Hans"}[WantsTraditional(lower)]]
	}
	if len(lower) > 2 {
		lower = lower[:2]
	}
	return lower, Seeds[lower]
}

// WantsTraditional is whether this language tag asks for Traditional
// characters. A script subtag is the answer when the tag has one — `zh-Hans-HK`
// is Simplified written in Hong Kong — and the region is the answer when not.
func WantsTraditional(tag string) bool {
	lower := strings.ToLower(tag)
	if !strings.HasPrefix(lower, "zh") || strings.Contains(lower, "hans") {
		return false
	}
	for _, mark := range []string{"tw", "hk", "hant", "mo"} {
		if strings.Contains(lower, mark) {
			return true
		}
	}
	return false
}

// Plan is what this transcriber will give whisper, and who decided it.
func (t Transcriber) Plan() Plan { return Decide(t.Language, t.Follow) }

// Transcribe reads one recording and answers with what was said.
//
// An empty string with no error is a real answer and the commonest one for a
// pocket: whisper heard the recording and there were no words in it. A failure
// is reserved for the four things that are actually wrong.
func (t Transcriber) Transcribe(ctx context.Context, samples []byte) (string, error) {
	if len(samples) <= floorBytes() {
		return "", ErrTooShort
	}
	binary, model, err := t.Ready()
	if err != nil {
		return "", err
	}

	// Serial, and the wait is inside the caller's context: a request that has
	// already been abandoned should not take the lock and then run a model.
	if err := lock(ctx); err != nil {
		return "", err
	}
	defer running.Unlock()

	// Hand over the part with speech in it, and nothing else.
	speech := TrimSilence(samples, Rate, 2.5)
	if len(speech) <= floorBytes() {
		return "", nil
	}

	wav, err := t.writeWAV(speech)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrFailed, err)
	}
	// The temporary file goes whatever happens next, including a panic on the
	// way out: a directory slowly filling with somebody's speech is not a
	// thing this is allowed to leave behind.
	defer os.Remove(wav)

	plan := t.Plan()
	args := []string{
		"-m", model, "-f", wav,
		"-l", plan.Code,
		"-nt",         // no timestamps: this is a prompt, not a subtitle file
		"-np",         // no progress bar in the output we are about to parse
		"-sns",        // drop non-speech tokens rather than writing them down
		"-nth", "0.8", // and be harder to convince that room tone was a word
	}
	// The seed, and nothing else. **No vocabulary**: the prompt is a sample of
	// the text that came before, so a prompt that is mostly a word list
	// produces a transcript with no punctuation in it at all — measured on one
	// clip in `Whisper.swift`, and punctuation is worth more here than a nudge
	// towards spellings the model is already good at. The names are repaired
	// afterwards instead, by ApplyVocabulary, which cannot make a sentence
	// worse.
	if plan.Seed != "" {
		args = append(args, "--prompt", plan.Seed)
	}

	timeout := t.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	run := t.Run
	if run == nil {
		run = runProcess
	}
	out, err := run(bounded, binary, args...)
	if err != nil {
		if bounded.Err() != nil && ctx.Err() == nil {
			return "", ErrTimeout
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("%w: %v", ErrFailed, err)
	}

	text := Clean(string(out))
	// A groove is not a sentence, and answering with one is worse than
	// answering with nothing.
	if LooksLikeLoop(text) {
		return "", nil
	}
	// The guarantee behind the seed, and behind `auto` too: when whisper was
	// left to detect the language, what it wrote is converted only if it came
	// back Chinese.
	if plan.Script == "Hant" && (plan.Code == "zh" || LooksChinese(text)) {
		text = ToTraditional(text)
	}
	text = Tidy(text)
	return ApplyVocabulary(text, append(append([]string{}, AlwaysExpected...), t.Vocabulary...)), nil
}

// lock takes the one slot, or gives up when the caller does.
func lock(ctx context.Context) error {
	got := make(chan struct{})
	go func() {
		running.Lock()
		close(got)
	}()
	select {
	case <-got:
		return nil
	case <-ctx.Done():
		// The goroutine still takes the lock when its turn comes, so it is
		// handed straight back rather than held by nobody.
		go func() {
			<-got
			running.Unlock()
		}()
		return ctx.Err()
	}
}

func floorBytes() int { return int(float64(Rate) * Floor.Seconds() * 2) }

// runProcess is one whisper-cli run. Only standard output is read: whisper
// writes its load messages and its timings to standard error, and mixing the
// two would put "load_backend: …" in somebody's message.
func runProcess(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	// Nothing from this process's own standard input reaches it, and the
	// working directory is left alone: the model and the WAV are both
	// absolute, so there is nothing for a relative path to resolve against.
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return out, nil
}

// writeWAV puts the samples in a file with a 44-byte header in front of them.
//
// In the daemon's own directory when it has one, so a recording is never
// written somewhere a shared /tmp makes readable, and with 0600 on it. The
// name carries no part of what was said.
func (t Transcriber) writeWAV(samples []byte) (string, error) {
	dir := t.TempDir
	if dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
	}
	f, err := os.CreateTemp(dir, "clawdline-voice-*.wav")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := os.Chmod(f.Name(), 0o600); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	if _, err := f.Write(WAV(samples, Rate)); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Sync(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// WAV is `Whisper.wavData`: a 44-byte PCM header in front of the samples,
// written by hand rather than by reaching for a library, which keeps the whole
// conversion in one place — where the bugs would be.
func WAV(samples []byte, rate int) []byte {
	const channels, bits = 1, 16
	out := make([]byte, 0, 44+len(samples))
	u32 := func(v uint32) { out = binary.LittleEndian.AppendUint32(out, v) }
	u16 := func(v uint16) { out = binary.LittleEndian.AppendUint16(out, v) }

	out = append(out, "RIFF"...)
	u32(uint32(36 + len(samples)))
	out = append(out, "WAVE"...)
	out = append(out, "fmt "...)
	u32(16)
	u16(1)
	u16(channels)
	u32(uint32(rate))
	u32(uint32(rate * channels * bits / 8))
	u16(channels * bits / 8)
	u16(bits)
	out = append(out, "data"...)
	u32(uint32(len(samples)))
	return append(out, samples...)
}

// Clean is what `Whisper.transcribe` does to whisper-cli's output: every line
// trimmed, blank lines and bracketed ones dropped, the rest joined with a
// space. A bracketed line is a timestamp or a `[BLANK_AUDIO]` marker, neither
// of which is something anybody said.
func Clean(out string) string {
	parts := []string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.Trim(line, " \t\r")
		if line == "" || strings.HasPrefix(line, "[") {
			continue
		}
		parts = append(parts, line)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// TrimSilence cuts the quiet off both ends (`Whisper.trimSilence`).
//
// Whisper does not answer "there was nothing there". Given silence it
// continues whatever context it has — it recites the initial prompt back at
// you, or falls into repeating one phrase for as long as the silence lasts.
// Neither is fixable by asking; the fix is to not hand it silence.
//
// **The threshold is relative to the room and not to the loudest moment**, and
// getting that backwards ate the end of every sentence: a quarter of the peak
// is twelve decibels down, ordinary speech has thirty decibels of range in it,
// and a sentence falls away as it ends. The symptom was not a wrong
// transcription, it was a correct transcription of slightly less than you
// said, which is far harder to notice.
func TrimSilence(samples []byte, rate int, overRoom float64) []byte {
	count := len(samples) / 2
	if count <= int(float64(rate)*0.2) {
		return samples
	}
	window := max(1, int(float64(rate)*0.05)) // 50 ms
	energies := []float64{}
	for i := 0; i < count; i += window {
		end := min(i+window, count)
		sum := 0.0
		for j := i; j < end; j++ {
			v := int16(binary.LittleEndian.Uint16(samples[j*2:]))
			sum += math.Abs(float64(v))
		}
		energies = append(energies, sum/float64(end-i))
	}
	loudest := 0.0
	for _, e := range energies {
		loudest = math.Max(loudest, e)
	}
	if loudest <= 0 {
		return nil
	}
	// The quietest tenth stands in for the room. A percentile rather than the
	// minimum: one freak-quiet window would otherwise set the level for the
	// whole clip.
	sorted := append([]float64{}, energies...)
	sort.Float64s(sorted)
	room := sorted[min(len(sorted)-1, len(sorted)/10)]
	// One level all the way through: there is no room tone here to tell
	// speech apart from, so there is nothing this can honestly cut.
	if loudest <= room*overRoom {
		return samples
	}
	// The second term matters in a recording with no room tone at all, where
	// `room` is near zero and the first term would keep every stray bit. Two
	// per cent of the peak is about thirty-four decibels down — permissive on
	// purpose, because including a little room costs nothing and cutting a
	// word costs the word.
	floor := math.Max(room*overRoom, loudest*0.02)
	first, last := -1, -1
	for i, e := range energies {
		if e > floor {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return nil
	}
	// A breath of room either side, so words are not clipped at their edges.
	// 300ms rather than 150: a consonant that starts a word is quieter than
	// the vowel behind it, and the padding is what stops the threshold from
	// shaving it off.
	const pad = 6
	from := max(0, first-pad) * window
	to := min(count, (last+1+pad)*window)
	if to <= from {
		return nil
	}
	return samples[from*2 : to*2]
}

// LooksLikeLoop is whether a transcript is the model stuck in a groove
// (`Whisper.looksLikeLoop`).
//
// "和音，和音，和音，和音，" is not a sentence anybody said; it is what happens
// when the decoder has nothing to go on and keeps choosing the same
// continuation. Cheaper to spot afterwards than to prevent.
func LooksLikeLoop(text string) bool {
	cleaned := strings.ReplaceAll(text, " ", "")
	// The clause check first, and with no length floor: the shortest real
	// example measured is "好,好,好。" — six characters, and unmistakably not a
	// sentence.
	parts := strings.FieldsFunc(cleaned, func(r rune) bool {
		return strings.ContainsRune("，,。.、;；！!？?", r)
	})
	kept := parts[:0]
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	// Three, not four. A person saying one clause three times, punctuated, is
	// not something worth protecting from.
	if len(kept) >= 3 {
		same := true
		for _, p := range kept[1:] {
			if p != kept[0] {
				same = false
				break
			}
		}
		if same {
			return true
		}
	}
	chars := []rune(cleaned)
	if len(chars) < 8 {
		return false
	}
	// Or one phrase repeated until it fills most of the output.
	for unit := 1; unit <= 6; unit++ {
		if len(chars) < unit*4 {
			continue
		}
		piece := string(chars[:unit])
		repeats, i := 0, 0
		for i+unit <= len(chars) && string(chars[i:i+unit]) == piece {
			repeats++
			i += unit
		}
		if repeats >= 4 && float64(repeats*unit)/float64(len(chars)) > 0.7 {
			return true
		}
	}
	return false
}

// ToTraditional turns Simplified characters into Traditional ones and leaves
// everything else alone (`Whisper.toTraditional`).
//
// The initial prompt only biases the script; asking for Traditional and
// getting a sentence of Simplified back is a thing that happens, and this is
// the guarantee behind it. It converts characters and not vocabulary: 网络
// becomes 網絡 and not 網路. Wording is a regional choice and this is not the
// layer that can make it. See traditional.go for where the table came from and
// exactly how it differs from ICU's.
func ToTraditional(text string) string {
	if !strings.ContainsFunc(text, isHan) {
		return text
	}
	return strings.Map(func(r rune) rune {
		if t, ok := simplifiedToTraditional[r]; ok {
			return t
		}
		return r
	}, text)
}

// Tidy puts the spaces and the punctuation back (`Whisper.tidy`).
//
// Whisper writes Chinese without a space before the English inside it —
// 那個webhook的retry — and reaches for a half-width comma in the middle of a
// Chinese sentence. Neither is something the model can be asked to stop doing;
// both are mechanical to repair. A space between Han characters and Latin ones
// is a typographic convention old enough to have a name, and it is the
// difference between a sentence you read and one you decode.
func Tidy(text string) string {
	chars := []rune(text)
	var out strings.Builder
	for i, ch := range chars {
		if i > 0 {
			p := chars[i-1]
			// Only between the two scripts, and never doubling a space
			// already there.
			boundary := (isHan(p) && isLatin(ch)) || (isLatin(p) && isHan(ch))
			if boundary && !unicode.IsSpace(p) && !unicode.IsSpace(ch) {
				out.WriteRune(' ')
			}
		}
		// A comma with Chinese on either side belongs to the Chinese
		// sentence, even when the word in front of it happens to be English —
		// which is most of them here.
		touchesHan := (i > 0 && isHan(chars[i-1])) || (i+1 < len(chars) && isHan(chars[i+1]))
		if full, ok := fullWidth[ch]; ok && touchesHan {
			out.WriteRune(full)
			continue
		}
		out.WriteRune(ch)
	}
	return out.String()
}

var fullWidth = map[rune]rune{
	',': '，', ';': '；', ':': '：', '?': '？', '!': '！',
}

func isHan(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF) || (r >= 0xF900 && r <= 0xFAFF)
}

func isLatin(r rune) bool {
	if r >= 0x2E80 {
		return false
	}
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

// ApplyVocabulary puts back the words that are not words
// (`Whisper.applyVocabulary`).
//
// **Not done in the prompt, and that is measured rather than preferred.**
// `--prompt` is a sample of the text that came before, not a list of terms,
// and the transcript imitates it: on one clip, changing nothing else, a prompt
// of seed-plus-twenty-words produced a transcript with not one punctuation
// mark in it. Spelling is worth less than punctuation, and this repair is
// mechanical anyway.
//
// Conservative on purpose. A term shorter than four letters is matched exactly
// or not at all, because at that length everything is within one edit of
// everything.
func ApplyVocabulary(text string, terms []string) string {
	if text == "" {
		return text
	}
	words := WordRanges(text)
	if len(words) == 0 {
		return text
	}
	type want struct{ term, key string }
	wanted := []want{}
	for _, term := range terms {
		key := normalise(term)
		if len([]rune(key)) >= 4 {
			wanted = append(wanted, want{term, key})
		}
	}
	// Longest first, so "Claude Code" is settled before "Claude" can claim
	// half of it. A stable sort, so two terms of the same length are always
	// tried in the order they were given.
	sort.SliceStable(wanted, func(i, j int) bool {
		return len([]rune(wanted[i].key)) > len([]rune(wanted[j].key))
	})

	type swap struct {
		from, to int // byte offsets into text
		with     string
	}
	swaps := []swap{}
	claimed := map[int]bool{}
	for _, w := range wanted {
		key := []rune(w.key)
		// How wrong a word is allowed to be and still count. Calibrated
		// against the two failures that matter, in both directions:
		// "cloudline" is two edits from "clawdline" and has to match, and
		// "claw" is one edit from "Clawd" and must not. So a short term buys
		// no tolerance at all — at five letters, one edit reaches every
		// ordinary word nearby.
		tolerance := 0
		switch {
		case len(key) >= 9:
			tolerance = 2
		case len(key) >= 6:
			tolerance = 1
		}
		span := len(strings.Fields(w.term))
		for size := 1; size <= span+1; size++ {
			if size > len(words) {
				break
			}
			for start := 0; start+size <= len(words); start++ {
				taken := false
				for i := start; i < start+size; i++ {
					if claimed[i] {
						taken = true
						break
					}
				}
				if taken {
					continue
				}
				joined := ""
				for i := start; i < start+size; i++ {
					joined += normalise(text[words[i].from:words[i].to])
				}
				// Nothing to do when it is already right, and a bad idea to
				// try: an exact match differing only in case is usually the
				// writer's own capitalisation.
				if joined == w.key || joined == "" {
					continue
				}
				if abs(len([]rune(joined))-len(key)) > tolerance || Distance(joined, w.key) > tolerance {
					continue
				}
				for i := start; i < start+size; i++ {
					claimed[i] = true
				}
				swaps = append(swaps, swap{words[start].from, words[start+size-1].to, w.term})
			}
		}
	}
	sort.Slice(swaps, func(i, j int) bool { return swaps[i].from > swaps[j].from })
	out := text
	for _, s := range swaps {
		out = out[:s.from] + s.with + out[s.to:]
	}
	return out
}

// Span is one run of letters and digits in a string, in byte offsets.
type Span struct{ from, to int }

// WordRanges are the runs of ASCII letters and digits (`Whisper.wordRanges`).
// Everything between them — spaces, punctuation, Han characters — is a
// boundary, which is what makes this work inside a Chinese sentence.
func WordRanges(text string) []Span {
	out := []Span{}
	start := -1
	for i, r := range text {
		latin := r < 0x80 && (unicode.IsLetter(r) || unicode.IsNumber(r))
		if latin && start < 0 {
			start = i
		}
		if !latin && start >= 0 {
			out = append(out, Span{start, i})
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, Span{start, len(text)})
	}
	return out
}

func normalise(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Distance is Levenshtein on strings a few characters long. Two rows rather
// than a matrix, as the Swift app keeps it.
func Distance(a, b string) int {
	x, y := []rune(a), []rune(b)
	if len(x) == 0 {
		return len(y)
	}
	if len(y) == 0 {
		return len(x)
	}
	prev := make([]int, len(y)+1)
	row := make([]int, len(y)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(x); i++ {
		row[0] = i
		for j := 1; j <= len(y); j++ {
			if x[i-1] == y[j-1] {
				row[j] = prev[j-1]
			} else {
				row[j] = min(prev[j-1], min(prev[j], row[j-1])) + 1
			}
		}
		prev, row = row, prev
	}
	return prev[len(y)]
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
