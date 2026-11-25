package http

import (
	"errors"
	"testing"

	"github.com/sainteye/clawdline/internal/app/browserbridge"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
)

// A Bot is told which argument was wrong and what is allowed, not only the code.
func TestBotBrowserFailureNamesTheRefusedField(t *testing.T) {
	err := browserFailure(&browserbridge.ArgError{Code: browserbridge.BadRequest, Field: "width", Message: "width must be a whole number of CSS pixels from 200 to 3840"})
	var got *app.BrowserFailure
	if !errors.As(err, &got) || got.Code != "browser_bad_request" || got.Message != "width: width must be a whole number of CSS pixels from 200 to 3840" {
		t.Fatalf("failure = %+v", got)
	}
	err = browserFailure(&browserbridge.ArgError{Code: browserbridge.NotFound, Field: "label", Message: `no visible element matches "Xavia"`})
	if !errors.As(err, &got) || got.Code != "browser_element_not_found" {
		t.Fatalf("not found = %+v", got)
	}
}
