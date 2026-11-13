package projectassistant

import (
	"context"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type fakeReadViews struct {
	query  ReadViewQuery
	view   ReadView
	calls  int
	during func()
}

func (f *fakeReadViews) Read(_ context.Context, q ReadViewQuery) (ReadView, error) {
	f.query = q
	f.calls++
	if f.during != nil {
		f.during()
	}
	return f.view, nil
}

func serviceReadViewFixture(t *testing.T) (*serviceFixture, *fakeReadViews, pa.ReadViewRequest) {
	t.Helper()
	f := newServiceFixture(t)
	b := f.create(t)
	c := f.repo.conversation
	port := &fakeReadViews{view: ReadView{Mode: pa.ReadViewSnapshot, SourceEpoch: "reader-incarnation",
		Head: ReadViewHead{Key: f.key, Project: b.Project, Conversation: c,
			CurrentFence: pa.Fence{Key: f.key, Project: b.Project, ConversationID: c.ID}, BotVersion: 1},
		Snapshot: &ReadSnapshot{Bot: b}}}
	f.s.opts.ReadViews = port
	f.s.opts.ReadViewByteLimit = cloudops.ReadPlaintextBytesLimit
	return f, port, pa.ReadViewRequest{Key: f.key, ConversationID: c.ID, Mode: pa.ReadViewSnapshot, PageSize: 100}
}

func TestServiceReadViewFreshAuthorityAtReadAndPublication(t *testing.T) {
	changes := map[string]func(*Authority){
		"principal":       func(a *Authority) { a.PrincipalID = "other" },
		"reader":          func(a *Authority) { a.ReaderID = "other" },
		"revision":        func(a *Authority) { a.AuthorizationRevision = "new" },
		"machine":         func(a *Authority) { a.Source.MachineID = "other" },
		"revoke":          func(a *Authority) { a.Source.State = pa.SourceRevoked },
		"offline":         func(a *Authority) { a.Source.State = pa.SourceOffline },
		"expired":         func(a *Authority) { a.Source.Fresh = false },
		"read ceiling":    func(a *Authority) { a.Source.Readable = false },
		"write downgrade": func(a *Authority) { a.Source.Writable = false },
		"feature downgrade": func(a *Authority) {
			a.Feature.Enabled = false
			a.Feature.HasData = true
			a.Source.Access = pa.HistoryOnly
		},
		"hidden": func(a *Authority) { a.Feature = pa.FeatureState{}; a.Source.Access = pa.Hidden },
	}
	for name, change := range changes {
		for _, phase := range []string{"before", "during", "publication"} {
			t.Run(name+"/"+phase, func(t *testing.T) {
				f, port, q := serviceReadViewFixture(t)
				if phase == "before" {
					change(&f.authority)
				}
				if phase == "during" {
					port.during = func() { change(&f.authority) }
				}
				result, err := f.s.View(context.Background(), q)
				if phase == "before" && (name == "principal" || name == "reader" || name == "revision" || name == "write downgrade" || name == "feature downgrade") {
					if err != nil {
						t.Fatal(err)
					}
					return // current readable authority is valid
				}
				if phase == "publication" {
					if err != nil {
						t.Fatal(err)
					}
					change(&f.authority)
					err = result.Revalidate(context.Background())
				}
				if err == nil {
					t.Fatal("changed authority published content")
				}
				if f.repo.exports != 0 || f.runtime.calls != 0 {
					t.Fatal("read invoked aggregate/provider")
				}
			})
		}
	}
}

func TestServiceReadViewBindingsBudgetsAndOptionalComposition(t *testing.T) {
	f, port, q := serviceReadViewFixture(t)
	result, err := f.s.View(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if port.query.ByteLimit != cloudops.ReadPlaintextBytesLimit || port.query.CursorByteLimit != pa.MaxReadViewCursorBytes || port.query.AuthorizationBinding == "" || port.query.Request != q {
		t.Fatalf("untrusted query: %+v", port.query)
	}
	binding := port.query.AuthorizationBinding
	f.authority.Source.ObservedAt = time.Now()
	if err = result.Revalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.View(context.Background(), q); err != nil || port.query.AuthorizationBinding != binding {
		t.Fatalf("timestamp invalidated stable binding: %v", err)
	}
	f.authority.ReaderID = "new-reader"
	if _, err = f.s.View(context.Background(), q); err != nil || port.query.AuthorizationBinding == binding {
		t.Fatalf("reader missing from binding: %v", err)
	}
	f.s.opts.ReadViews = nil
	if _, err = f.s.View(context.Background(), q); pa.Code(err) != pa.Unsupported {
		t.Fatal(err)
	}
	if _, _, _, err = f.s.Get(context.Background(), q.Key); err != nil {
		t.Fatalf("old method broken: %v", err)
	}
	if err = (ReadViewResult{}).Revalidate(context.Background()); pa.Code(err) != pa.ScopeDenied {
		t.Fatal(err)
	}
}

func TestServiceReadViewRejectsForeignStoredIdentity(t *testing.T) {
	for _, name := range []string{"key", "project", "conversation", "conversation-key", "fence", "fence-generation", "epoch", "mode"} {
		t.Run(name, func(t *testing.T) {
			f, port, q := serviceReadViewFixture(t)
			switch name {
			case "key":
				port.view.Head.Key.BotID = "other"
			case "project":
				port.view.Head.Project.MachineID = "other"
			case "conversation":
				port.view.Head.Conversation.ID = "other"
			case "conversation-key":
				port.view.Head.Conversation.Key.BotID = "other"
			case "fence":
				port.view.Head.CurrentFence.Project.CanonicalID = "other"
			case "fence-generation":
				port.view.Head.CurrentFence.Generation++
			case "epoch":
				port.view.SourceEpoch = ""
			case "mode":
				port.view.Mode = pa.ReadViewHistory
			}
			if _, err := f.s.View(context.Background(), q); pa.Code(err) != pa.ScopeDenied {
				t.Fatalf("foreign identity: %v", err)
			}
		})
	}
}
