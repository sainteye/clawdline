package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/whisper"
	"github.com/sainteye/clawdline/internal/contract"
)

// POST /v1/voice: a recording made in a browser, turned into words by this
// machine and nobody else's (`RemoteServer.transcribe`).
//
// **The point of the route is where the audio does not go.** Every phone
// already has a recogniser behind a permission prompt, and what that costs is
// that the sentence goes to whoever wrote the recogniser. This project's
// position is stated everywhere else it comes up: the text stays on the
// machine. So the browser records, the samples come here, and the model that
// reads them is the one already on this disk. No client may name a different
// one — neither the binary, nor the model, nor the language is taken from the
// request. The language is this machine's to decide (`whisper` below).
//
// **Answered off the request's own path, and that is the design rather than an
// optimisation.** Whisper takes about a second and a half warm and twelve
// after a reboot, so the gates are checked here, where their state is, and the
// serial part happens in the adapter behind one lock. The queue in front of it
// is what bounds the wait; two in the line and the third is told to come back,
// because a third recording accepted now would be answered five seconds after
// it was spoken, by which time its author has pressed the button again.
//
// Nothing on this path can reach a terminal. That is what makes a dictation
// that heard the wrong thing a typo rather than an incident.

// voiceDepth is one running, one waiting, and the third is told to come back.
// Not a resource limit — it is how long somebody is willing to hold a phone.
const voiceDepth = 2

// voiceCeiling is the far end, and it is about cost rather than size: whisper
// runs at something near real time, so an eight-minute upload holds the queue
// for minutes with nobody able to call it back. Five minutes is far past
// anything somebody says in one breath.
const voiceCeiling = 300 * time.Second

// voiceBodyLimit is the Swift app's body limit for this route. A minute of
// 16 kHz Int16 is 1.9MB, 2.6MB once base64 has had it, so five minutes of
// audio fits inside this with room to spare — and anything that does not is
// refused before it is decoded.
const voiceBodyLimit = 20 << 20

// voiceQueued is how many recordings are on the queue. Counted here, where
// the refusal is written, rather than inside the adapter, which knows about
// one run and not about the line in front of it.
var voiceQueued = struct {
	sync.Mutex
	n int
}{}

func (s *Server) voiceRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"a transcription is a POST; a GET would make it something a link could do by accident")
		return
	}
	// **Write, not read, and the reason is not that this writes anything.** It
	// is that a device which may only read has nowhere to put a sentence — it
	// cannot send one — while transcribing costs this machine ten seconds of
	// every core it has. Read-level access is meant to be cheap to grant; this
	// is not.
	s.voiceWriting(w, r, func(w http.ResponseWriter) { s.transcribe(w, r) })
}

// voiceWriting is `writing` for this one route, with the Swift app's rule
// about which answers are worth keeping.
//
// **A 429 and a 503 are not filed, and neither is filed for the same reason.**
// Both are about this machine at this moment rather than about the request —
// the queue drains, whisper gets installed — and an answer frozen for ten
// minutes would mean the retry that was supposed to work is told "busy" by a
// cache long after the queue emptied. The table exists so that a retry cannot
// repeat an effect, and a refusal had no effect to repeat.
func (s *Server) voiceWriting(w http.ResponseWriter, r *http.Request, body func(http.ResponseWriter)) {
	if !maySend(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "This device may read, and not send.")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That needs an Idempotency-Key header.")
		return
	}
	// The recording is the request: its bytes are the digest, so a key
	// reused for a different recording is refused rather than answered with
	// what somebody said the first time (D03). Read once, here, up to one
	// byte past the limit — voiceSamples still says 413 for the rest.
	raw, err := io.ReadAll(io.LimitReader(r.Body, voiceBodyLimit+1))
	if err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not a recording")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	k := store.ReceiptKey{Scope: scopeVoice, Actor: accessOf(r).verdict.Device, Key: key}
	s.receipted(w, r, k, requestDigest(raw),
		func(status int) bool {
			return status != http.StatusTooManyRequests && status != http.StatusServiceUnavailable
		},
		body)
}

func (s *Server) transcribe(w http.ResponseWriter, r *http.Request) {
	samples, ok := voiceSamples(w, r)
	if !ok {
		return
	}

	// Not filed, and checked before anything expensive: a full queue is the
	// smaller answer and the more useful one, because it can be acted on.
	if !voiceEnqueue() {
		writeRefusal(w, http.StatusTooManyRequests, "busy",
			"Two recordings are already waiting to be transcribed on this machine. Try again in a moment.")
		return
	}
	defer voiceLeave()

	engine := voiceEngine(s)
	started := time.Now()
	// The request's own context, so a browser that gave up — Cancel while it
	// was reading — is not still holding the one whisper slot.
	ctx, cancel := context.WithTimeout(r.Context(), voiceCeiling+whisper.DefaultTimeout)
	defer cancel()
	text, err := engine.Transcribe(ctx, samples)
	ms := time.Since(started).Milliseconds()
	seconds := float64(len(samples)) / float64(whisper.Rate*2)

	if err != nil {
		// Written where every other refusal on this daemon is written, and
		// without one character of what was said: the seconds and the failure
		// are the whole of what a person diagnosing this needs.
		log.Printf("audit voice.transcribe seconds=%.1f ms=%d ok=0 why=%s", seconds, ms, voiceCode(err))
		writeVoiceFailure(w, err)
		return
	}
	// Which language it was read as, and who said so: the question a
	// Simplified transcript raises, answered without one word of it.
	plan := engine.Plan()
	log.Printf("audit voice.transcribe seconds=%.1f ms=%d chars=%d ok=1 lang=%s script=%s from=%s",
		seconds, ms, len([]rune(text)), plan.Code, plan.Script, plan.From.Source)
	// **Nothing heard is a 200.** Whisper answers with nothing for silence,
	// for a clip it decided was a groove, and for a recording of a room — and
	// none of those is a failure of the request. What was asked is "what was
	// said", and "nothing" is an answer to that; a 4xx here would have a page
	// apologising for a microphone that was working perfectly.
	writeJSON(w, contract.VoiceResult{Text: text, Ms: ms})
}

// voiceSamples reads the body and decides everything that can be decided
// before any of it costs a second of somebody's CPU.
//
// The rate is read before the audio is decoded — the cheap field first,
// because a body that names the wrong rate is a body whose megabytes are not
// worth turning into bytes.
func voiceSamples(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	var body contract.VoiceRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, voiceBodyLimit))
	if err := dec.Decode(&body); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeRefusal(w, http.StatusRequestEntityTooLarge, "too_large",
				"That recording is larger than this route takes. Send it in pieces.")
			return nil, false
		}
		writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not a recording")
		return nil, false
	}
	// Checked rather than resampled: 16 kHz is what the recorder produces and
	// what whisper wants, so a client sending 48 kHz has not made a small
	// mistake — it has sent something that would transcribe as a voice three
	// times too fast. Refusing says that; resampling would hide it.
	if body.Rate != whisper.Rate {
		writeRefusal(w, http.StatusBadRequest, "bad_request",
			"rate must be 16000. That is the only rate this transcribes, and quietly "+
				"resampling somebody's voice is a worse answer than saying no.")
		return nil, false
	}
	if strings.TrimSpace(body.Audio) == "" {
		writeRefusal(w, http.StatusBadRequest, "bad_request",
			"That needs audio: base64 of little-endian 16-bit mono PCM.")
		return nil, false
	}
	samples, ok := decodeAudio(body.Audio)
	if !ok || len(samples) == 0 {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "That audio was not base64.")
		return nil, false
	}
	seconds := float64(len(samples)) / float64(whisper.Rate*2)
	if seconds < whisper.Floor.Seconds() {
		writeRefusal(w, http.StatusBadRequest, "bad_request",
			"That was under a quarter of a second of audio, which is a tap and not a sentence.")
		return nil, false
	}
	if seconds > voiceCeiling.Seconds() {
		writeRefusal(w, http.StatusBadRequest, "bad_request",
			"That recording is longer than five minutes, which is the limit here. Send it in pieces.")
		return nil, false
	}
	return samples, true
}

// decodeAudio is base64 with unknown characters ignored, because an encoder
// that wraps its lines is not a client making a mistake — and with the padding
// optional, because more than one encoder leaves it off. Something that is not
// base64 at all still arrives as nothing, which is the refusal underneath.
func decodeAudio(encoded string) ([]byte, bool) {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r == '+' || r == '/' || r == '=':
			return r
		// The URL-safe alphabet, folded onto the standard one rather than
		// refused: it is the same bytes spelled differently.
		case r == '-':
			return '+'
		case r == '_':
			return '/'
		}
		return -1
	}, encoded)
	clean = strings.TrimRight(clean, "=")
	out, err := base64.RawStdEncoding.DecodeString(clean)
	if err != nil {
		return nil, false
	}
	return out, true
}

func voiceEnqueue() bool {
	voiceQueued.Lock()
	defer voiceQueued.Unlock()
	if voiceQueued.n >= voiceDepth {
		return false
	}
	voiceQueued.n++
	return true
}

func voiceLeave() {
	voiceQueued.Lock()
	defer voiceQueued.Unlock()
	voiceQueued.n--
}

// voiceEngine is how this route reaches the transcriber. A variable rather
// than a direct call so that a test can point the search at an empty directory
// and ask for the "not installed" and "no model" answers, without moving or
// consulting what is actually installed on the machine running it.
var voiceEngine = (*Server).whisper

// voiceMachine is how `auto` reads this machine's languages. A variable so a
// test can be a machine set to zh-CN, or one that says nothing, on a machine
// that is neither — this one is zh-TW, which is exactly where a wrong rule
// passes by accident.
var voiceMachine = whisper.Machine{}

// whisper builds the transcriber from this machine's own settings, which are
// the Swift app's key spellings in this app's own file: `whisper_binary`,
// `whisper_model`, `voice_language` and `voice_vocabulary`. A file that cannot
// be read is not a refusal — it means every key has its default, which is the
// ordinary first-run state.
//
// **`auto` follows Clawdline, and the request still names nothing.** The
// language comes from this machine's side of the wire — its own `language`
// setting, then its own locale, then the catalog it serves (whisper/locale.go)
// — so a recording from the hosted console on a phone, which is a different
// build with its own idea of the interface's language, is read exactly as one
// from the page this daemon serves. A client that could name a language could
// also be an old client that names none, and the answer would depend on which
// build somebody happened to have open.
func (s *Server) whisper() whisper.Transcriber {
	out := whisper.Transcriber{
		Language: "auto",
		// The recording is written inside this daemon's own directory rather
		// than a shared temporary one, so somebody's speech never lands
		// somewhere the rest of the machine can read.
		TempDir: filepath.Join(s.cfg.Dir, "voice"),
	}
	values, err := s.settingsFile().Read()
	out.Follow = voiceFollow(values, err == nil)
	if err != nil {
		return out
	}
	if v, ok := values.String("whisper_binary"); ok {
		out.Binary = v
	}
	if v, ok := values.String("whisper_model"); ok {
		out.Model = v
	}
	if v, ok := values.String("voice_language"); ok && v != "" {
		out.Language = v
	}
	if raw, ok := values.Raw["voice_vocabulary"]; ok {
		var words []string
		// A hand-edited list with a number in it is not a reason to refuse to
		// transcribe: the key is left at its default and the rest of the file
		// still counts.
		if json.Unmarshal(raw, &words) == nil {
			out.Vocabulary = words
		}
	}
	return out
}

// voiceFollow is who `auto` asks, in order: Clawdline's own language when the
// file names one, this machine's languages, and last the catalog this daemon
// serves, which is what every page it draws is written in when nothing
// narrows it.
func voiceFollow(values nextconfig.Values, readable bool) []whisper.Answer {
	follow := []whisper.Answer{}
	if readable {
		if v, ok := values.String("language"); ok && whisper.Usable(v) {
			follow = append(follow, whisper.Answer{Tag: v, Source: whisper.FromLanguage})
		}
	}
	follow = append(follow, voiceMachine.Languages(context.Background())...)
	return append(follow, whisper.Answer{Tag: defaultCatalog, Source: whisper.FromCatalog})
}

// voiceLanguageRoute is GET /v1/voice/language: what the next recording will
// be read as, and who said so. The settings window says it back under the
// picker, so "auto" is never a word standing in for an answer nobody can see.
func (s *Server) voiceLanguageRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET")
		return
	}
	engine := voiceEngine(s)
	plan := engine.Plan()
	setting := engine.Language
	if strings.TrimSpace(setting) == "" {
		setting = "auto"
	}
	writeJSON(w, contract.VoiceLanguage{
		Setting: setting, Code: plan.Code, Script: plan.Script,
		Source: plan.From.Source, Tag: plan.From.Tag,
		ScriptSource: plan.ScriptFrom.Source, ScriptTag: plan.ScriptFrom.Tag,
	})
}

// voiceCode is the machine-readable half of each failure, and the only part
// anything branches on.
func voiceCode(err error) string {
	switch {
	case errors.Is(err, whisper.ErrNoBinary), errors.Is(err, whisper.ErrNoModel):
		return "no_whisper"
	case errors.Is(err, whisper.ErrTimeout):
		return "voice_timeout"
	case errors.Is(err, whisper.ErrTooShort):
		return "bad_request"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "voice_abandoned"
	}
	return "voice_failed"
}

// writeVoiceFailure gives each named failure the status that describes it.
//
// `no_whisper` carries `reason`, because the two halves of it are two
// different afternoons: `brew install whisper-cpp` leaves a machine with the
// binary and no model, and "no whisper" would send somebody to check the thing
// they already did.
func writeVoiceFailure(w http.ResponseWriter, err error) {
	code := voiceCode(err)
	switch code {
	case "no_whisper":
		reason, detail := "no_binary", "This machine has no whisper-cli, so there is nothing here to read a recording with."
		if errors.Is(err, whisper.ErrNoModel) {
			reason = "no_model"
			detail = "whisper-cli is installed on this machine and has no model to read with. " +
				"It only needs one ggml file in ~/.cache/whisper."
		}
		writeVoiceRefusal(w, http.StatusServiceUnavailable, code, detail, reason)
	case "voice_timeout":
		writeVoiceRefusal(w, http.StatusGatewayTimeout, code,
			"whisper-cli was still reading that recording when its time ran out.", "")
	case "bad_request":
		writeVoiceRefusal(w, http.StatusBadRequest, code,
			"That was too short to read: under a quarter of a second is a tap, not a sentence.", "")
	case "voice_abandoned":
		// Nobody is listening for this one — the request went away — but a
		// status is still written rather than a silence, so the replay table
		// files something a retry can be told.
		writeVoiceRefusal(w, http.StatusRequestTimeout, code, "That request was abandoned before it was answered.", "")
	default:
		// The Swift app answers a failed run with an empty transcript, which
		// reads to the person as "nothing was said". It is named here instead:
		// a model that fell over and a room that was quiet are different
		// facts, and only one of them is worth trying again.
		writeVoiceRefusal(w, http.StatusBadGateway, code,
			"whisper-cli did not finish reading that recording.", "")
	}
}

func writeVoiceRefusal(w http.ResponseWriter, status int, code, detail, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(contract.VoiceRefusal{Error: code, Detail: detail, Reason: reason})
}
