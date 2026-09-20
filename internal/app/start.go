package app

import (
	"context"
	"errors"
	"net/http"
	"os"

	"github.com/sainteye/clawdline-go/internal/adapters/projects"
	"github.com/sainteye/clawdline-go/internal/adapters/terminal"
	"github.com/sainteye/clawdline-go/internal/app/ports"
)

// Starter is StartPoints.start and StartPoints.resume: open a terminal where a
// place is and run an assistant in it.
//
// This is the only thing on this daemon that creates execution from a request,
// and its shape is the Swift app's for that reason: a place is an id resolved
// against a list built now, the assistant is one of two literals, a model is a
// closed name, a conversation is a UUID this machine has just listed for that
// place, and the terminal is chosen here from what is running rather than
// named by the caller.
type Starter struct {
	// Places is the place list, built at the moment of asking.
	Places func(ctx context.Context) []projects.Place
	// Terminal is the machine's `terminal` setting.
	Terminal func() projects.TerminalChoice
	Launcher ports.Launcher
	// Past lists what an assistant has already recorded in a place.
	Past func(ctx context.Context, place projects.Place, assistant string) []projects.Past
}

// Started is StartPoints.Outcome.started. Attach is filled for the one plan
// that puts a session where nobody is looking: a tmux server started detached.
type Started struct {
	ID      string
	Backend string
	Attach  string
}

// StartRefusal is StartPoints.Outcome.refused: an HTTP status, the code a page
// branches on, an English sentence, and the terminal's name when the refusal
// is about one.
type StartRefusal struct {
	Status  int
	Code    string
	Message string
	App     string
}

func (r StartRefusal) Error() string { return r.Code + ": " + r.Message }

func notFound(message string) StartRefusal {
	return StartRefusal{Status: http.StatusNotFound, Code: "not_found", Message: message}
}

// Place is StartPoints.place(withID:): resolved against a list built now, so a
// directory deleted since the page last looked is the same 404 as an id that
// was never real.
func (s Starter) Place(ctx context.Context, id string) (projects.Place, bool) {
	if id == "" {
		return projects.Place{}, false
	}
	for _, p := range s.Places(ctx) {
		if p.ID == id {
			return p, true
		}
	}
	return projects.Place{}, false
}

// Start is StartPoints.start(_:assistant:model:…resume:).
func (s Starter) Start(ctx context.Context, place projects.Place, assistant, model, resume string) (Started, error) {
	launch, err := projects.Admit(projects.LaunchRequest{ProjectRoot: place.Path, Assistant: assistant,
		Model: model, Resume: resume})
	if err != nil {
		if errors.Is(err, projects.ErrInvalidLaunch) {
			return Started{}, StartRefusal{Status: http.StatusBadRequest, Code: "invalid_launch", Message: err.Error()}
		}
		return Started{}, err
	}
	if !placeIsDirectory(place.Path) {
		return Started{}, notFound("No place named that")
	}
	// Each fact asked once and held: asking twice is a second subprocess that
	// may by then answer differently.
	itermOpen, _ := s.Launcher.ITermRunning(ctx)
	reach := projects.TmuxReach(s.Launcher.TmuxReach(ctx))
	plan := projects.ChoosePlan(s.Terminal(), itermOpen, reach)

	switch plan {
	case projects.PlanITerm:
		id, err := s.Launcher.NewITermTab(ctx, launch.ShellLine())
		if err != nil {
			return Started{}, openRefusal(err, "iTerm2")
		}
		return Started{ID: id, Backend: "iterm"}, nil
	case projects.PlanTmux:
		id, err := s.Launcher.NewTmuxWindow(ctx, place.Path, launch.ShellCommand())
		if err != nil {
			return Started{}, openRefusal(err, "")
		}
		return Started{ID: id, Backend: "tmux"}, nil
	case projects.PlanTmuxDetached:
		id, err := s.Launcher.NewTmuxSession(ctx, place.Path, projects.TmuxStartedSessionName, launch.ShellCommand())
		if err != nil {
			return Started{}, openRefusal(err, "")
		}
		return Started{ID: id, Backend: "tmux", Attach: projects.TmuxAttachCommand()}, nil
	case projects.PlanNotRunning:
		// StartPoints.appName: the name as a person says it. This daemon does
		// not ask Launch Services for the display name; iTerm2's is its name.
		return Started{}, StartRefusal{Status: http.StatusConflict, Code: "terminal_closed",
			Message: "iTerm2 is not running, and this will not launch it for you. Open it on the machine and try again.",
			App:     "iTerm2"}
	default:
		return Started{}, StartRefusal{Status: http.StatusConflict, Code: "terminal_unsupported",
			Message: "tmux is the terminal for new sessions in Settings, and there is no tmux on this machine. " +
				"Install tmux, or pick a different terminal in Settings — see docs/remote.md."}
	}
}

// Resume is StartPoints.resume(_:sessionID:assistant:): the conversation has to
// be one this machine lists for that place right now, and then it is a start
// with one more flag.
//
// The Swift app also accepts a conversation proven by a terminal schedule run
// (Orchestrator.scheduledResumeTitle) and remembers the resumed title for the
// new terminal (CodexNaming.rememberResumedTitle). This daemon has neither
// record, so a conversation off the ordinary list is the only one it resumes.
func (s Starter) Resume(ctx context.Context, place projects.Place, sessionID, assistant string) (Started, error) {
	id, ok := projects.SessionName(sessionID)
	if !ok {
		return Started{}, notFound("No conversation named that")
	}
	found := false
	for _, p := range s.Past(ctx, place, assistant) {
		if p.ID == id {
			found = true
			break
		}
	}
	if !found {
		return Started{}, notFound("No conversation named that")
	}
	return s.Start(ctx, place, assistant, "", id)
}

// openRefusal is the Swift mapping from a terminal failure to a refusal:
// 501 for a platform that cannot open that terminal at all, 502 otherwise.
func openRefusal(err error, app string) StartRefusal {
	var unsupported terminal.Unsupported
	if errors.As(err, &unsupported) {
		return StartRefusal{Status: http.StatusNotImplemented, Code: "capability_unavailable", Message: err.Error()}
	}
	var failure terminal.Failure
	if errors.As(err, &failure) && failure.Attention && app != "" {
		return StartRefusal{Status: http.StatusBadGateway, Code: "iterm_attention_required",
			Message: failure.Message, App: app}
	}
	return StartRefusal{Status: http.StatusBadGateway, Code: "terminal_io_failed", Message: err.Error()}
}

func placeIsDirectory(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}
