package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/artifacts"
	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// fakeHost records every act on a session, in order, and can be told to fail
// one of them.
type fakeHost struct {
	sessions []session.Session
	mu       sync.Mutex
	acts     []string
	failKey  bool
}

func (h *fakeHost) Name() string { return "tmux" }
func (h *fakeHost) Inventory(ctx context.Context) (session.Inventory, error) {
	return session.Inventory{Complete: true, Sessions: h.sessions}, nil
}
func (h *fakeHost) Open(ctx context.Context, req ports.OpenRequest) (session.Session, error) {
	return session.Session{}, errors.New("no")
}
func (h *fakeHost) Send(ctx context.Context, s session.Session, text string) error {
	h.note("send:" + text)
	return nil
}
func (h *fakeHost) Interrupt(ctx context.Context, s session.Session) error { return nil }
func (h *fakeHost) Close(ctx context.Context, s session.Session) error     { return nil }
func (h *fakeHost) Type(ctx context.Context, s session.Session, text string) error {
	h.note("type:" + text)
	return nil
}
func (h *fakeHost) Keystroke(ctx context.Context, s session.Session, b []byte) error {
	if h.failKey {
		return errors.New("the pane is gone")
	}
	h.note("key:" + base64.StdEncoding.EncodeToString(b))
	return nil
}
func (h *fakeHost) note(act string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.acts = append(h.acts, act)
}

type fakeLender struct {
	mu        sync.Mutex
	acts      []string
	refuse    map[string]bool
	borrowErr error
	hold      chan struct{}
}

func (l *fakeLender) Available() bool { return true }
func (l *fakeLender) Borrow(ctx context.Context) (*artifacts.Borrowed, error) {
	if l.hold != nil {
		<-l.hold
	}
	if l.borrowErr != nil {
		return nil, l.borrowErr
	}
	l.note("borrow")
	return &artifacts.Borrowed{}, nil
}
func (l *fakeLender) Offer(ctx context.Context, b *artifacts.Borrowed, path string) error {
	if l.refuse[filepath.Base(path)] || len(l.refuse) > 0 && l.refuse["*"] {
		return errors.New("not an image")
	}
	l.note("offer")
	return nil
}
func (l *fakeLender) GiveBack(ctx context.Context, b *artifacts.Borrowed) error {
	l.note("giveback")
	return nil
}
func (l *fakeLender) note(act string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.acts = append(l.acts, act)
}

func pngURL(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, 0, color.RGBA{R: 255, A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func pictureFixture(t *testing.T, assistant session.Assistant) (Actions, *fakeHost, *fakeLender, string) {
	t.Helper()
	pictureSettle, submitSettle = 0, 0
	host := &fakeHost{sessions: []session.Session{{ID: "%7", TTY: "ttys007", Backend: session.BackendTmux, Assistant: assistant}}}
	lender := &fakeLender{refuse: map[string]bool{}}
	dir := t.TempDir()
	a := Actions{
		Inventory: Inventory{Terminals: []ports.TerminalHost{host}},
		Terminals: []ports.TerminalHost{host},
		Pictures:  Pictures{Drops: artifacts.NewDrops(dir), Pasteboard: lender},
	}
	return a, host, lender, artifacts.DropsDir(dir)
}

func files(t *testing.T, dir string) []string {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	out := []string{}
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

var (
	ctrlV  = "key:" + base64.StdEncoding.EncodeToString([]byte{0x16})
	enter  = "key:" + base64.StdEncoding.EncodeToString([]byte{0x0d})
	anyURL = "data:text/plain;base64,aGVsbG8="
)

// Into Claude Code the words go first, then each picture is lent to the
// pasteboard and pasted with Ctrl-V, then one Return; the pasteboard is handed
// back after the Return, and the files stay for the program to read.
func TestPicturesArePastedIntoClaudeInOrder(t *testing.T) {
	a, host, lender, drops := pictureFixture(t, session.AssistantClaude)
	if _, err := a.SendWithPictures(context.Background(), "%7", "什麼顏色", []string{pngURL(t, 3, 2), pngURL(t, 2, 2)}); err != nil {
		t.Fatal(err)
	}
	want := []string{"type:什麼顏色", ctrlV, ctrlV, enter}
	if strings.Join(host.acts, "|") != strings.Join(want, "|") {
		t.Fatalf("acts %q", host.acts)
	}
	if strings.Join(lender.acts, ",") != "borrow,offer,offer,giveback" {
		t.Fatalf("pasteboard %q", lender.acts)
	}
	kept := files(t, drops)
	if len(kept) != 2 {
		t.Fatalf("kept %v", kept)
	}
	info, err := os.Stat(drops)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("drops dir %v %v", info.Mode(), err)
	}
	f, _ := os.Stat(filepath.Join(drops, kept[0]))
	if f.Mode().Perm() != 0o600 {
		t.Fatalf("drop file %v", f.Mode())
	}
}

// Codex is given the paths, after the words, in one ordinary send.
func TestPicturesGoToCodexAsPaths(t *testing.T) {
	a, host, lender, drops := pictureFixture(t, session.AssistantCodex)
	if _, err := a.SendWithPictures(context.Background(), "%7", "看這個", []string{pngURL(t, 1, 1)}); err != nil {
		t.Fatal(err)
	}
	kept := files(t, drops)
	if len(host.acts) != 1 || host.acts[0] != "send:看這個 "+filepath.Join(drops, kept[0]) {
		t.Fatalf("acts %q", host.acts)
	}
	if len(lender.acts) != 0 {
		t.Fatalf("the pasteboard was touched for Codex: %q", lender.acts)
	}
}

// A picture that cannot be decoded is left out; a send none of whose pictures
// can be is refused, and nothing is typed or kept.
func TestUndecodablePictures(t *testing.T) {
	a, host, _, drops := pictureFixture(t, session.AssistantClaude)
	if _, err := a.SendWithPictures(context.Background(), "%7", "hi", []string{anyURL, pngURL(t, 1, 1)}); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.Join(host.acts, "|"), ctrlV); n != 1 {
		t.Fatalf("pasted %d, acts %q", n, host.acts)
	}
	a, host, _, drops = pictureFixture(t, session.AssistantClaude)
	_, err := a.SendWithPictures(context.Background(), "%7", "hi", []string{anyURL, "file:///etc/passwd", "data:image/png;base64,AAAA"})
	if ref, ok := err.(Refusal); !ok || ref.Code != "bad_request" || !strings.Contains(ref.Detail, "None of those") {
		t.Fatalf("err %v", err)
	}
	if len(host.acts) != 0 || len(files(t, drops)) != 0 {
		t.Fatalf("acts %q files %v", host.acts, files(t, drops))
	}
}

// A picture the pasteboard will not take goes as its path, in its place.
func TestARefusedOfferFallsBackToThePath(t *testing.T) {
	a, host, lender, drops := pictureFixture(t, session.AssistantClaude)
	lender.refuse["*"] = true
	if _, err := a.SendWithPictures(context.Background(), "%7", "", []string{pngURL(t, 1, 1)}); err != nil {
		t.Fatal(err)
	}
	kept := files(t, drops)
	want := []string{"type:" + filepath.Join(drops, kept[0]), enter}
	if strings.Join(host.acts, "|") != strings.Join(want, "|") {
		t.Fatalf("acts %q", host.acts)
	}
	// A pasteboard that cannot be borrowed at all sends the whole message as paths.
	a, host, lender, _ = pictureFixture(t, session.AssistantClaude)
	lender.borrowErr = errors.New("no pasteboard")
	if _, err := a.SendWithPictures(context.Background(), "%7", "x", []string{pngURL(t, 1, 1)}); err != nil {
		t.Fatal(err)
	}
	if len(host.acts) != 1 || !strings.HasPrefix(host.acts[0], "send:x /") {
		t.Fatalf("acts %q", host.acts)
	}
}

// A keystroke that does not land is a typed failure, the pasteboard is still
// given back, and the files that never reached a program are removed.
func TestAFailedKeystrokeDiscardsTheFiles(t *testing.T) {
	a, host, lender, drops := pictureFixture(t, session.AssistantClaude)
	host.failKey = true
	_, err := a.SendWithPictures(context.Background(), "%7", "x", []string{pngURL(t, 1, 1)})
	if ref, ok := err.(Refusal); !ok || ref.Code != "send_failed" {
		t.Fatalf("err %v", err)
	}
	if got := strings.Join(lender.acts, ","); got != "borrow,offer,giveback" {
		t.Fatalf("pasteboard %q", got)
	}
	if left := files(t, drops); len(left) != 0 {
		t.Fatalf("left %v", left)
	}
}

// The pasteboard is one thing: past a short queue the answer is `busy` before
// anything is typed, and a caller that gives up while queued types nothing.
func TestPictureSendsQueueAndThenRefuse(t *testing.T) {
	a, host, lender, _ := pictureFixture(t, session.AssistantClaude)
	lender.hold = make(chan struct{})
	url := pngURL(t, 1, 1)
	done := make(chan error, 1)
	go func() {
		_, err := a.SendWithPictures(context.Background(), "%7", "first", []string{url})
		done <- err
	}()
	// Wait until the first send holds the pasteboard.
	for len(pasteboardSlot) == 0 {
		time.Sleep(time.Millisecond)
	}
	waiting := make(chan error, pasteboardQueue)
	ctx, cancel := context.WithCancel(context.Background())
	for range pasteboardQueue {
		go func() {
			_, err := a.SendWithPictures(ctx, "%7", "queued", []string{url})
			waiting <- err
		}()
	}
	for pasteboardWaiting.Load() < pasteboardQueue {
		time.Sleep(time.Millisecond)
	}
	_, err := a.SendWithPictures(context.Background(), "%7", "one too many", []string{url})
	if ref, ok := err.(Refusal); !ok || ref.Code != "busy" {
		t.Fatalf("err %v", err)
	}
	cancel()
	for range pasteboardQueue {
		if err := <-waiting; err == nil {
			t.Fatal("a cancelled queued send went ahead")
		} else if ref, ok := err.(Refusal); !ok || ref.Code != "busy" {
			t.Fatalf("queued err %v", err)
		}
	}
	close(lender.hold)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for _, act := range host.acts {
		if strings.Contains(act, "queued") || strings.Contains(act, "too many") {
			t.Fatalf("typed %q", act)
		}
	}
}

func TestQuotedPath(t *testing.T) {
	for in, want := range map[string]string{
		"/tmp/a-b_c.png":      "/tmp/a-b_c.png",
		"/tmp/with space.png": "'/tmp/with space.png'",
		"/tmp/it's.png":       `'/tmp/it'\''s.png'`,
	} {
		if got := quotedPath(in); got != want {
			t.Fatalf("%q: %q", in, got)
		}
	}
}
