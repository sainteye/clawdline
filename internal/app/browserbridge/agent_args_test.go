package browserbridge

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

// argsRecorder answers every command and keeps the arguments the extension
// would have received.
func argsRecorder(t *testing.T, answer func(Command) Result) (*Bridge, func() []Command, func(op string, args string) (json.RawMessage, error)) {
	t.Helper()
	b, ctx := connected(t)
	var mu sync.Mutex
	var sent []Command
	fakeHost(ctx, b, func(c Command) Result {
		mu.Lock()
		sent = append(sent, c)
		mu.Unlock()
		if answer != nil {
			return answer(c)
		}
		return Result{ID: c.ID, OK: true, Tab: 7, Value: json.RawMessage(`{}`)}
	})
	if _, err := b.Do(ctx, "c1", "open", json.RawMessage(`{"url":"https://example.com/"}`)); err != nil {
		t.Fatal(err)
	}
	seen := func() []Command {
		mu.Lock()
		defer mu.Unlock()
		return append([]Command(nil), sent...)
	}
	do := func(op, args string) (json.RawMessage, error) { return b.Do(ctx, "c1", op, json.RawMessage(args)) }
	return b, seen, do
}

func lastArgs(t *testing.T, seen []Command, op string) map[string]any {
	t.Helper()
	for i := len(seen) - 1; i >= 0; i-- {
		if seen[i].Op == op {
			var out map[string]any
			if err := json.Unmarshal(seen[i].Args, &out); err != nil {
				t.Fatal(err)
			}
			return out
		}
	}
	t.Fatalf("no %s command reached the extension", op)
	return nil
}

func argField(t *testing.T, err error) *ArgError {
	t.Helper()
	var arg *ArgError
	if !errors.As(err, &arg) {
		t.Fatalf("err = %v (%T), want an *ArgError", err, err)
	}
	return arg
}

// A Bot's documented args.label reaches the extension as the target it reads.
func TestAgentClickLabelReachesTheExtensionAsItsTarget(t *testing.T) {
	_, seen, do := argsRecorder(t, nil)
	if _, err := do("click", `{"label":"Xavia"}`); err != nil {
		t.Fatal(err)
	}
	got := lastArgs(t, seen(), "click")
	if got["target"] != "Xavia" || got["label"] != nil {
		t.Fatalf("click args = %v, want target Xavia and no label", got)
	}
}

// A label the page does not show is a typed not-found, not a page error.
func TestAgentClickLabelNotFoundIsATypedRefusal(t *testing.T) {
	_, _, do := argsRecorder(t, func(c Command) Result {
		if c.Op == "click" {
			return Result{ID: c.ID, Tab: 7, Error: `no visible element matches "所有 Bot"; read the page for the exact labels`}
		}
		return Result{ID: c.ID, OK: true, Tab: 7, Value: json.RawMessage(`{}`)}
	})
	_, err := do("click", `{"label":"所有 Bot"}`)
	if r, ok := IsRefusal(err); !ok || r != NotFound {
		t.Fatalf("err = %v, want %s", err, NotFound)
	}
	if arg := argField(t, err); arg.Field != "label" || !strings.Contains(arg.Message, "所有 Bot") {
		t.Fatalf("not-found = %+v", arg)
	}
}

// Nothing without a usable point or label is handed to the extension, so it
// can never send Input.dispatchMouseEvent without x.
func TestAgentClickWithoutAPointIsRefusedBeforeTheExtension(t *testing.T) {
	_, seen, do := argsRecorder(t, nil)
	for args, field := range map[string]string{
		`{}`:                         "label",
		`{"y":10}`:                   "x",
		`{"x":10}`:                   "y",
		`{"x":"31","y":733}`:         "x",
		`{"x":-1,"y":5}`:             "x",
		`{"x":5,"y":1e9}`:            "y",
		`{"label":""}`:               "label",
		`{"label":"a","target":"b"}`: "label",
		`{"label":7}`:                "label",
	} {
		_, err := do("click", args)
		if r, ok := IsRefusal(err); !ok || r != BadRequest {
			t.Fatalf("%s: err = %v, want %s", args, err, BadRequest)
		}
		if arg := argField(t, err); arg.Field != field || arg.Message == "" {
			t.Fatalf("%s: refusal = %+v, want field %s with a message", args, arg, field)
		}
	}
	for _, c := range seen() {
		if c.Op == "click" {
			t.Fatalf("a refused click reached the extension: %s", c.Args)
		}
	}
}

// A point in CSS pixels is passed through as numbers; the extension sends the
// move, press and release there.
func TestAgentClickPointIsPassedThroughAsCSSPixels(t *testing.T) {
	_, seen, do := argsRecorder(t, nil)
	if _, err := do("click", `{"x":31.5,"y":733}`); err != nil {
		t.Fatal(err)
	}
	got := lastArgs(t, seen(), "click")
	if got["x"] != 31.5 || got["y"] != 733.0 {
		t.Fatalf("click args = %v", got)
	}
}

func TestAgentViewportAcceptsASizeAndNamesTheWrongField(t *testing.T) {
	_, seen, do := argsRecorder(t, nil)
	if _, err := do("viewport", `{"width":390,"height":844,"mobile":true}`); err != nil {
		t.Fatal(err)
	}
	got := lastArgs(t, seen(), "viewport")
	if got["width"] != 390.0 || got["height"] != 844.0 || got["mobile"] != true {
		t.Fatalf("viewport args = %v", got)
	}
	if _, err := do("viewport", `{"width":1280,"height":800}`); err != nil {
		t.Fatal(err)
	}
	for _, preset := range []string{`{"mode":"mobile"}`, `{"mode":"desktop"}`, `{"preset":"mobile"}`} {
		if _, err := do("viewport", preset); err != nil {
			t.Fatalf("%s: %v", preset, err)
		}
	}
	if got := lastArgs(t, seen(), "viewport"); got["mode"] != "mobile" || got["preset"] != nil {
		t.Fatalf("preset alias args = %v, want mode mobile", got)
	}
	for args, field := range map[string]string{
		`{}`:                                         "mode",
		`{"mode":"tablet"}`:                          "mode",
		`{"width":390}`:                              "height",
		`{"height":844}`:                             "width",
		`{"width":99999,"height":844}`:               "width",
		`{"width":390,"height":10}`:                  "height",
		`{"width":390.5,"height":844}`:               "width",
		`{"width":390,"height":844,"mobile":1}`:      "mobile",
		`{"width":390,"height":844,"touch":"y"}`:     "touch",
		`{"mode":"mobile","width":390,"height":844}`: "mode",
		`{"width":390,"height":844,"scale":3}`:       "scale",
	} {
		_, err := do("viewport", args)
		if r, ok := IsRefusal(err); !ok || r != BadRequest {
			t.Fatalf("%s: err = %v, want %s", args, err, BadRequest)
		}
		if arg := argField(t, err); arg.Field != field || arg.Message == "" {
			t.Fatalf("%s: refusal = %+v, want field %s", args, arg, field)
		}
	}
}
