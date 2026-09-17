//go:build darwin

package artifacts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Pasteboard lends pictures to the macOS general pasteboard, which is how
// Claude Code is given one: it reads the pasteboard when it receives Ctrl-V and
// shows the picture as `[Image #1]` (`Targets.send(_ pieces:to:)`).
//
// It is one shared thing that belongs to the person at the keyboard, so every
// use is Borrow, Offer…, GiveBack, and the caller serialises them.
type Pasteboard struct{}

func NewPasteboard() Pasteboard { return Pasteboard{} }

// Available is whether this platform has a pasteboard to lend.
func (Pasteboard) Available() bool { return true }

// pasteboardScript is the three commands, through the Objective-C bridge. The
// path and the saved bytes are arguments and standard input, never script text.
const pasteboardScript = `
ObjC.import("AppKit");
function run(argv) {
  const pb = $.NSPasteboard.generalPasteboard;
  const cmd = String(argv[0] || "");
  if (cmd === "save") {
    const items = pb.pasteboardItems;
    const out = [];
    if (items && !items.isNil()) {
      for (let i = 0; i < items.count; i++) {
        const item = items.objectAtIndex(i);
        const types = item.types;
        const fields = [];
        for (let j = 0; j < types.count; j++) {
          const t = types.objectAtIndex(j);
          const d = item.dataForType(t);
          if (d && !d.isNil()) {
            fields.push({ type: ObjC.unwrap(t), data: ObjC.unwrap(d.base64EncodedStringWithOptions(0)) });
          }
        }
        out.push(fields);
      }
    }
    return JSON.stringify({ ok: true, items: out, change: Number(pb.changeCount) });
  }
  if (cmd === "offer") {
    const img = $.NSImage.alloc.initWithContentsOfFile($(String(argv[1] || "")));
    if (!img || img.isNil() || !img.isValid) return JSON.stringify({ ok: false, error: "not an image" });
    pb.clearContents;
    const ok = pb.writeObjects($.NSArray.arrayWithObject(img));
    return JSON.stringify({ ok: !!ok, change: Number(pb.changeCount) });
  }
  if (cmd === "restore") {
    const expected = Number(argv[1]);
    if (Number(pb.changeCount) !== expected) return JSON.stringify({ ok: true, skipped: true });
    const input = $.NSFileHandle.fileHandleWithStandardInput.readDataToEndOfFile;
    const text = ObjC.unwrap($.NSString.alloc.initWithDataEncoding(input, $.NSUTF8StringEncoding));
    const saved = JSON.parse(text || "[]");
    pb.clearContents;
    if (!saved.length) return JSON.stringify({ ok: true });
    const list = $.NSMutableArray.array;
    saved.forEach(function (fields) {
      const item = $.NSPasteboardItem.alloc.init;
      fields.forEach(function (f) {
        const d = $.NSData.alloc.initWithBase64EncodedStringOptions($(f.data), 0);
        if (d && !d.isNil()) item.setDataForType(d, $(f.type));
      });
      list.addObject(item);
    });
    return JSON.stringify({ ok: !!pb.writeObjects(list) });
  }
  return JSON.stringify({ ok: false, error: "unknown command" });
}
`

type pasteboardAnswer struct {
	OK      bool            `json:"ok"`
	Error   string          `json:"error"`
	Items   json.RawMessage `json:"items"`
	Change  int64           `json:"change"`
	Skipped bool            `json:"skipped"`
}

func runPasteboard(ctx context.Context, stdin []byte, args ...string) (pasteboardAnswer, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript",
		append([]string{"-l", "JavaScript", "-e", pasteboardScript}, args...)...)
	cmd.Stdin = bytes.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return pasteboardAnswer{}, fmt.Errorf("the pasteboard did not answer: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var answer pasteboardAnswer
	if json.Unmarshal(bytes.TrimSpace(out), &answer) != nil {
		return pasteboardAnswer{}, errors.New("the pasteboard answered something that is not JSON")
	}
	if !answer.OK {
		return answer, fmt.Errorf("the pasteboard refused: %s", answer.Error)
	}
	return answer, nil
}

// Borrow takes a copy of everything on the pasteboard.
func (Pasteboard) Borrow(ctx context.Context) (*Borrowed, error) {
	answer, err := runPasteboard(ctx, nil, "save")
	if err != nil {
		return nil, err
	}
	return &Borrowed{items: answer.Items, change: answer.Change}, nil
}

// Offer puts one picture on the pasteboard. A file that does not load as an
// image is an error, and the caller hands that one over as a path instead.
func (Pasteboard) Offer(ctx context.Context, b *Borrowed, path string) error {
	answer, err := runPasteboard(ctx, nil, "offer", path)
	if err != nil {
		return err
	}
	b.change = answer.Change
	return nil
}

// GiveBack puts back what Borrow copied — unless somebody has copied something
// since the last Offer, which is theirs and stays.
func (Pasteboard) GiveBack(ctx context.Context, b *Borrowed) error {
	items := b.items
	if len(items) == 0 {
		items = json.RawMessage("[]")
	}
	_, err := runPasteboard(ctx, items, "restore", fmt.Sprint(b.change))
	return err
}
