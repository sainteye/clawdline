package cloudops

import (
	"context"
	"testing"
)

func TestPackageCloudWordsKeepPublicReadsSeparateFromPrivateWrites(t *testing.T) {
	read := func(word string, pkg map[string]any) (*router, Answer) {
		r := &router{}
		answer := Bridge{MachineID: "mac-01", Router: r}.Handle(context.Background(),
			request(t, ClassCtl, map[string]any{"type": word, "session": MachineReplySession,
				"request": "one", "package": pkg}))
		return r, answer
	}
	r, answer := read("squad.packages.preview", map[string]any{"archive_base64": "AA==", "scope_id": "global"})
	if answer.Status != 200 || len(r.seen) != 1 || r.last().Method != "POST" ||
		r.last().Path != "/v1/squad-packages/preview" {
		t.Fatalf("preview = %+v, %+v", answer, r.seen)
	}
	r, answer = read("squad.packages.export", map[string]any{"private_scopes": []any{}, "confirm_private": false})
	if answer.Status != 200 || len(r.seen) != 1 || r.last().Path != "/v1/squad-packages/export" {
		t.Fatalf("public export = %+v, %+v", answer, r.seen)
	}
	r, answer = read("squad.packages.export", map[string]any{"private_scopes": []any{"global"}, "confirm_private": true})
	if answer.Status == 200 || len(r.seen) != 0 {
		t.Fatalf("read word carried private data: %+v, %+v", answer, r.seen)
	}
	r, answer = read("squad.packages.export.private", map[string]any{"private_scopes": []any{"global"}, "confirm_private": true})
	if answer.Status == 200 || len(r.seen) != 0 {
		t.Fatalf("closed write succeeded: %+v, %+v", answer, r.seen)
	}
	opened := &router{}
	answer = open(opened).Handle(context.Background(), request(t, ClassCtl,
		map[string]any{"type": "squad.packages.export.private", "session": MachineReplySession,
			"request": "one", "package": map[string]any{"private_scopes": []any{"global"}, "confirm_private": true}}))
	if answer.Status != 200 || len(opened.seen) != 1 || opened.last().Path != "/v1/squad-packages/export" {
		t.Fatalf("private export = %+v, %+v", answer, opened.seen)
	}
}
