package app

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/antigravityprocess"
	"github.com/sainteye/clawdline/internal/adapters/antigravitysession"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// nativePictureHost is an owned protocol host: it receives kept picture files
// and must never be typed into.
type nativePictureHost struct {
	*fakeHost
	text     string
	pictures []string
	refuse   error
}

func (h *nativePictureHost) OwnsProtocol(session.Session) bool { return true }
func (h *nativePictureHost) SendProtocol(context.Context, session.Session, string, func() error) error {
	return errors.New("text-only send used for a picture")
}
func (h *nativePictureHost) SendProtocolPictures(ctx context.Context, s session.Session, text string, pictures []string, before func() error) error {
	if err := before(); err != nil {
		return err
	}
	for _, p := range pictures {
		if _, err := os.Stat(p); err != nil {
			return err
		}
	}
	h.text, h.pictures = text, pictures
	return h.refuse
}

func nativeFixture(t *testing.T) (Actions, *nativePictureHost, *fakeLender, string) {
	t.Helper()
	a, host, lender, drops := pictureFixture(t, session.AssistantGrok)
	native := &nativePictureHost{fakeHost: host}
	a.Inventory.Terminals[0], a.Terminals[0] = native, native
	return a, native, lender, drops
}

func TestGrokPicturesGoToTheNativePromptAsKeptFiles(t *testing.T) {
	a, host, lender, drops := nativeFixture(t)
	if _, err := a.SendWithPictures(context.Background(), "%7", "inspect this", []string{pngURL(t, 2, 2), pngURL(t, 1, 1)}); err != nil {
		t.Fatal(err)
	}
	if host.text != "inspect this" || len(host.pictures) != 2 {
		t.Fatalf("native prompt = %q %q", host.text, host.pictures)
	}
	if len(host.acts) != 0 || len(lender.acts) != 0 {
		t.Fatalf("a native picture was typed or pasted: %q %q", host.acts, lender.acts)
	}
	if len(files(t, drops)) != 2 {
		t.Fatal("an accepted picture's file was removed before the native prompt read it")
	}
}

func TestGrokPictureRefusedBeforeWriteRemovesItsFiles(t *testing.T) {
	a, host, _, drops := nativeFixture(t)
	host.refuse = &antigravitysession.Error{Code: "images_unsupported", Delivery: antigravityprocess.NotWritten}
	_, err := a.SendWithPictures(context.Background(), "%7", "", []string{pngURL(t, 1, 1)})
	var ref Refusal
	if !errors.As(err, &ref) || !errors.As(err, new(interface{ BeforeWrite() bool })) {
		t.Fatalf("refusal = %v", err)
	}
	if n := len(files(t, drops)); n != 0 {
		t.Fatalf("%d refused picture files kept", n)
	}
}

// Review 2026-11-11: pictures too large for one Grok prompt came back as a
// raw send_failed. They are a typed refusal the console translates.
func TestGrokPicturesTooLargeAreATypedRefusal(t *testing.T) {
	a, host, _, drops := nativeFixture(t)
	host.refuse = &antigravitysession.Error{Code: "pictures_too_large", Delivery: antigravityprocess.NotWritten}
	_, err := a.SendWithPictures(context.Background(), "%7", "six screenshots", []string{pngURL(t, 1, 1)})
	var ref Refusal
	if !errors.As(err, &ref) || ref.Code != "pictures_too_large" || ref.RawDetail || !errors.As(err, new(interface{ BeforeWrite() bool })) {
		t.Fatalf("refusal = %#v", err)
	}
	if n := len(files(t, drops)); n != 0 {
		t.Fatalf("%d refused picture files kept", n)
	}
}
