//go:build darwin || linux

package antigravityprocess

import (
	"context"
	"encoding/json"
	"reflect"
	"sync/atomic"
	"testing"
)

func TestRestoreUsesExactIdentityBeforeHistoryReplay(t *testing.T) {
	for _, replay := range []bool{false, true} {
		t.Run(map[bool]string{false: "resume", true: "load"}[replay], func(t *testing.T) {
			var seen atomic.Int32
			var c *Client
			var dir string
			c, dir = fake(t, "happy", func(_ context.Context, frame json.RawMessage) error {
				if c.SessionID() != "saved-exact" {
					t.Error("replay arrived before native identity was bound")
				}
				var m struct {
					Params struct {
						SessionID string `json:"sessionId"`
					}
				}
				if json.Unmarshal(frame, &m) != nil || m.Params.SessionID != "saved-exact" {
					t.Error("wrong replay identity")
				}
				seen.Add(1)
				return nil
			})
			ctx := context.Background()
			if _, err := c.Initialize(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := c.AuthenticatePersonal(ctx); err != nil {
				t.Fatal(err)
			}
			if !c.CanResume() {
				t.Fatal("native resume capability missing")
			}
			if _, err := c.ListSessions(ctx, "held-cursor"); err != nil {
				t.Fatal(err)
			}
			var err error
			method := "session/resume"
			if replay {
				_, err = c.LoadSession(ctx, "saved-exact")
				method = "session/load"
			} else {
				_, err = c.ResumeSession(ctx, "saved-exact")
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.SessionID() != "saved-exact" {
				t.Fatal("exact personal-quota native conversation not adopted")
			}
			want := int32(0)
			if replay {
				want = 1
			}
			if seen.Load() != want {
				t.Fatalf("history events=%d want=%d", seen.Load(), want)
			}
			if got := methods(t, dir); !reflect.DeepEqual(got, []string{"initialize", "authenticate", "session/list", method}) {
				t.Fatal(got)
			}
		})
	}
}

func TestRestoreFailureNeverCreatesOrPromptsAReplacement(t *testing.T) {
	for _, tc := range []struct{ mode, code string }{{"no-restore", "resume_unsupported"}, {"restore-error", "rpc_error"}, {"restore-wrong-id", "session_identity_changed"}} {
		t.Run(tc.mode, func(t *testing.T) {
			c, dir := fake(t, tc.mode, nil)
			ctx := context.Background()
			if _, err := c.Initialize(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := c.AuthenticatePersonal(ctx); err != nil {
				t.Fatal(err)
			}
			_, err := c.ResumeSession(ctx, "saved-exact")
			delivery := Acknowledged
			if tc.mode == "no-restore" {
				delivery = NotWritten
			}
			if tc.mode == "restore-error" {
				delivery = Acknowledged
			}
			requireError(t, err, tc.code, delivery)
			for _, method := range methods(t, dir) {
				if method == "session/new" || method == "session/prompt" {
					t.Fatal("replacement or prompt on restore failure", method)
				}
			}
		})
	}
}
