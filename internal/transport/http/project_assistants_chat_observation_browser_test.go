package http

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Only local protocol data is executed, never a provider CLI or model API.
type observationProvider struct {
	alignmentProvider
	finish        chan struct{}
	start         chan struct{}
	started       chan struct{}
	entered       sync.Once
	probes        atomic.Int32
	closeFailure  error
	promptFailure error
	offerDecision func(context.Context, pa.Turn) error
}

func (p *observationProvider) Capabilities(ctx context.Context, bot pa.Bot) (pa.Capabilities, error) {
	// Admission is the first probe. Hold the execution preflight before it
	// commits the new running generation, while real read views remain free.
	if p.probes.Add(1) > 1 {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		select {
		case <-p.start:
		case <-ctx.Done():
			return pa.Capabilities{}, ctx.Err()
		}
	}
	return p.alignmentProvider.Capabilities(ctx, bot)
}

func (p *observationProvider) Open(ctx context.Context, _ app.OpenRequest, emit func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	select {
	case <-p.start:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &observationSession{alignmentSession: &alignmentSession{provider: &p.alignmentProvider, emit: emit, answered: make(chan string, 1)}, owner: p}, nil
}

type observationSession struct {
	*alignmentSession
	owner *observationProvider
}

func (s *observationSession) Close(ctx context.Context) error {
	if s.owner.closeFailure != nil {
		return s.owner.closeFailure
	}
	return s.alignmentSession.Close(ctx)
}

func (s *observationSession) Send(_ context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	return observationPending{session: s, prompt: q}, nil
}

type observationPending struct {
	session *observationSession
	prompt  app.PromptRequest
}

func (p observationPending) Wait(ctx context.Context) (app.PromptResult, error) {
	turn := p.prompt.Turn
	if err := p.session.emit(ctx, app.ProtocolEvent{ID: "synthetic-observation-trace", Fence: turn.Fence, TurnID: turn.ID, Trace: &pa.Trace{ID: "synthetic-step", TurnID: turn.ID, Category: "tool", Text: "Synthetic observation tool output", Complete: true}}); err != nil {
		return app.PromptResult{}, err
	}
	p.session.owner.entered.Do(func() { close(p.session.owner.started) })
	select {
	case <-p.session.owner.finish:
		if offer := p.session.owner.offerDecision; offer != nil {
			if err := offer(ctx, turn); err != nil {
				return app.PromptResult{}, err
			}
		}
		if p.session.owner.promptFailure != nil {
			return app.PromptResult{}, p.session.owner.promptFailure
		}
		return app.PromptResult{FinalText: "Synthetic observation final answer"}, nil
	case <-ctx.Done():
		return app.PromptResult{}, ctx.Err()
	}
}

func TestProjectAssistantChatObservationBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_CHAT_OBSERVATION_BROWSER_DIR")
	if dir == "" {
		t.Skip("set CLAWDLINE_CHAT_OBSERVATION_BROWSER_DIR for the isolated accepted-send observation proof")
	}
	cases := []string{"hidden-en-desktop", "hidden-zh-mobile", "busy", "late-message", "read-failure", "leave", "revoke", "unknown", "malformed", "inspect", "cleanup-timeout", "quota-card", "decision-card", "decision-recovery", "interrupted-exchange"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(dir, name)
			if err := os.MkdirAll(out, 0700); err != nil {
				t.Fatal(err)
			}
			f := newPAServerFixture(t, true)
			provider := &observationProvider{finish: make(chan struct{}), start: make(chan struct{}), started: make(chan struct{})}
			if name == "cleanup-timeout" {
				provider.closeFailure = context.DeadlineExceeded
			}
			if name == "quota-card" {
				provider.promptFailure = &app.ProviderFailure{
					Reason: app.ProviderUsageExhausted,
					Cause:  &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: "synthetic_quota"},
				}
			}
			if name == "decision-card" || name == "decision-recovery" {
				store := projectassistantstore.New(f.s.store.ProjectAssistantDatabase())
				var once sync.Once
				provider.offerDecision = func(ctx context.Context, turn pa.Turn) (err error) {
					once.Do(func() {
						_, err = store.OfferDecision(ctx, turn.Fence, turn.ID, pa.AskPersonRequest{
							Question: "Which synthetic option should I use?", SuggestedReplies: []string{"Use the first option"},
						}, time.Now())
						if err == nil && name == "decision-recovery" {
							_, err = store.OfferDecision(ctx, turn.Fence, turn.ID, pa.AskPersonRequest{
								Question: "Which second option should I use?", SuggestedReplies: []string{"Use the second option"},
							}, time.Now())
						}
					})
					return err
				}
			}
			f.s.projectAssistants.provider = func() (app.Provider, error) { return provider, nil }
			f.enable(t)
			paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
			if name == "interrupted-exchange" {
				seedQueuedBotStatus(t, f, pa.TurnUnknown)
				paServerOK(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":false}`))
				f.enable(t)
			}
			source := httptest.NewServer(f.h)
			defer source.Close()
			var finish, start sync.Once
			cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/start" {
					start.Do(func() { close(provider.start) })
					ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
					defer cancel()
					select {
					case <-provider.started:
						w.WriteHeader(http.StatusNoContent)
					case <-ctx.Done():
						w.WriteHeader(http.StatusGatewayTimeout)
					}
					return
				}
				if r.URL.Path == "/finish" {
					finish.Do(func() { close(provider.finish) })
					w.WriteHeader(http.StatusNoContent)
					return
				}
				raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
				if err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				bridge := cloudops.Bridge{MachineID: f.key.MachineID, Router: groupUIRouter{origin: source.URL, token: f.local}, AllowCommands: func() bool { return true }}
				answer := bridge.Handle(r.Context(), cloudops.Command{Channel: "ctl/" + f.key.MachineID, Class: cloudops.ClassCtl, Sender: "synthetic-viewer", Sequence: 1, Plaintext: raw})
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(answer.Status)
				_, _ = w.Write(answer.Payload)
			}))
			defer cloud.Close()
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			command := exec.CommandContext(ctx, "node", "../../../web/console/tools/project-assistants-chat-observation.e2e.mjs")
			command.Env = append(os.Environ(), "BOTS_CLOUD_ORIGIN="+cloud.URL, "BOTS_MACHINE="+f.key.MachineID, "BOTS_ARTIFACTS="+out, "BOTS_CASE="+name)
			output, err := command.CombinedOutput()
			_ = os.WriteFile(filepath.Join(out, "browser.log"), output, 0600)
			if err != nil {
				t.Fatalf("chat observation: %v\n%s", err, output)
			}
			t.Log(string(output))
		})
	}
}
