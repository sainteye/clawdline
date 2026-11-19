package projectassistanthost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"runtime"
	"strings"

	process "github.com/sainteye/clawdline/internal/adapters/botdevserverprocess"
	serverstore "github.com/sainteye/clawdline/internal/adapters/botdevserverstore"
	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type serverReviewKey struct{}
type serverHumanAuthorityKey struct{}
type serverReview func(context.Context, pa.PermissionAction, []pa.PermissionOption) (pa.PermissionOption, error)

func (h *Host) serverProject(ctx context.Context, spec servers.Spec) (pa.ProjectResolution, error) {
	if _, _, err := h.background(ctx, spec.Key, true); err != nil {
		return pa.ProjectResolution{}, err
	}
	bot, err := h.store.Bot(ctx, spec.Key)
	if err != nil {
		return pa.ProjectResolution{}, err
	}
	if bot.Project != spec.Project {
		return pa.ProjectResolution{}, failure(pa.ScopeDenied, "server_project")
	}
	selection := pa.ProjectSelection{MachineID: spec.Project.MachineID, ProjectID: spec.Project.ProjectID}
	resolved, err := h.o.Projects.Resolve(ctx, selection)
	if err == nil {
		err = pa.ValidateTarget(selection, resolved)
	}
	if err == nil && resolved.Scope != spec.Project {
		err = failure(pa.ScopeDenied, "server_project")
	}
	return resolved, err
}

func (h *Host) serverControl(ctx context.Context, spec servers.Spec) error {
	if check, ok := ctx.Value(serverHumanAuthorityKey{}).(func(context.Context) error); ok {
		if err := check(ctx); err != nil {
			return err
		}
	}
	_, err := h.serverProject(ctx, spec)
	return err
}

func (h *Host) serverCheck(ctx context.Context, spec servers.Spec) error {
	if err := h.serverControl(ctx, spec); err != nil {
		return err
	}
	return h.serverMemory(ctx, spec, func() error { return nil })
}

// Enable owns this registry independently of any Engine/native lifetime.
// Reopened attempts remain unknown; this function never adopts or restarts one.
func (h *Host) makeServers(ctx context.Context) error {
	h.mu.Lock()
	prior := h.serverRegistry
	h.mu.Unlock()
	if prior != nil {
		u := prior.Usage()
		if !u.Known || u.Processes != 0 {
			return failure(pa.ExecutionEffectUnknown, "server_cleanup")
		}
		if err := prior.Close(ctx); err != nil {
			return err
		}
	}
	c, err := serverstore.NewCatalog(ctx, h.o.Database)
	if err != nil {
		return err
	}
	launch := h.o.ServerLaunch
	if launch == nil {
		launcher, err := process.New(acp.SourceProcessLimits(), os.Environ())
		if err != nil {
			return err
		}
		launch = launcher.Start
	}
	r, err := servers.OpenRegistry(ctx, servers.RegistryOptions{MachineID: h.o.MachineID, Lifetime: context.Background(), Catalog: c, ID: h.o.ID, CleanupTimeout: h.o.Limits.CleanupTimeout,
		CheckControl: h.serverControl,
		AuthorizeStart: func(ctx context.Context, saved servers.SavedServer) (func(context.Context) error, error) {
			review, ok := ctx.Value(serverReviewKey{}).(serverReview)
			if !ok {
				return nil, failure(pa.ScopeDenied, "server_review")
			}
			base, err := (servers.Approval{Rules: c, Check: h.serverCheck, Guard: h.serverMemory, Review: review}).Authorize(ctx, saved)
			if err != nil {
				return nil, err
			}
			return func(ctx context.Context) error {
				if check, ok := ctx.Value(serverHumanAuthorityKey{}).(func(context.Context) error); ok {
					if err := check(ctx); err != nil {
						return err
					}
				}
				return base(ctx)
			}, nil
		},
		Launch: func(ctx context.Context, spec servers.Spec) (p servers.Process, err error) {
			err = h.serverMemory(ctx, spec, func() error {
				resolved, e := h.serverProject(ctx, spec)
				if e != nil {
					return e
				}
				p, e = launch(ctx, spec, resolved)
				return e
			})
			return p, err
		}})
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.serverRegistry = r
	h.mu.Unlock()
	return nil
}

func serverInfo(saved servers.SavedServer) app.DevServerInfo {
	state := string(saved.Attempt.State)
	if state == "" {
		state = string(servers.Stopped)
	}
	fingerprint, _ := saved.Spec.Fingerprint()
	return app.DevServerInfo{Fingerprint: fingerprint, Revision: saved.Attempt.Version, ID: saved.ID, Name: saved.Spec.Name, Command: saved.Spec.Command, Folder: saved.Spec.Directory, Attempt: saved.Attempt.Identity.AttemptID, State: state, Failure: saved.Attempt.Failure, Ports: []app.DevServerPort{}}
}

func (h *Host) bindServerTools(q app.OpenRequest, life *workLifetime, tools *NativeWork, bound func(context.Context) (context.Context, context.CancelFunc, error), journal func(context.Context, string) error) error {
	check := func(ctx context.Context) error {
		ctx, done, err := bound(ctx)
		if err != nil {
			return err
		}
		defer done()
		return h.serverMemory(ctx, servers.Spec{Key: q.Bot.Key, Project: q.Bot.Project, ForgottenThrough: q.Binding.ForgottenThrough}, func() error { return nil })
	}
	caller, err := servers.NewCaller(life.sealed, check)
	if err != nil {
		return err
	}
	tools.serverPublish = func(ctx context.Context, publish func() error) error {
		if publish == nil {
			return failure(pa.Invalid, "server_publication")
		}
		ctx, done, err := bound(ctx)
		if err != nil {
			return err
		}
		defer done()
		return h.serverMemory(ctx, servers.Spec{Key: q.Bot.Key, Project: q.Bot.Project, ForgottenThrough: q.Binding.ForgottenThrough}, func() error {
			_, done, err := bound(ctx)
			if err != nil {
				return err
			}
			defer done()
			return publish()
		})
	}
	tools.serverAvailable = func(ctx context.Context) error {
		if runtime.GOOS == "windows" {
			return failure(pa.Unsupported, "server_platform")
		}
		if err := check(ctx); err != nil {
			return err
		}
		h.mu.Lock()
		available := h.enabled && !h.closed && h.serverRegistry != nil
		h.mu.Unlock()
		if !available {
			return failure(pa.Disabled, "server_runtime")
		}
		return nil
	}
	review := serverReview(tools.approveSource)
	tools.servers = func(ctx context.Context, request app.DevServerRequest) (app.DevServerResult, error) {
		if err := tools.serverAvailable(ctx); err != nil {
			return app.DevServerResult{}, err
		}
		ctx, done, err := bound(ctx)
		if err != nil {
			return app.DevServerResult{}, err
		}
		defer done()
		stop := context.AfterFunc(life.sealed, done)
		defer stop()
		h.mu.Lock()
		r := h.serverRegistry
		h.mu.Unlock()
		if r == nil {
			return app.DevServerResult{}, failure(pa.Disabled, "server_runtime")
		}
		inspect := func(ctx context.Context) ([]servers.SavedServer, error) {
			rows, err := r.Inspect(ctx, q.Bot.Key, q.Bot.Project)
			if err != nil {
				return nil, err
			}
			current := rows[:0]
			for _, saved := range rows {
				if saved.Spec.ForgottenThrough == q.Binding.ForgottenThrough {
					current = append(current, saved)
				}
			}
			return current, nil
		}
		find := func(ctx context.Context) (servers.SavedServer, error) {
			rows, err := inspect(ctx)
			if err != nil {
				return servers.SavedServer{}, err
			}
			for _, saved := range rows {
				if saved.ID == request.ID {
					return saved, nil
				}
			}
			return servers.SavedServer{}, failure(pa.NotFound, "server")
		}
		run := func(ctx context.Context) (json.RawMessage, error) {
			result := app.DevServerResult{Servers: []app.DevServerInfo{}}
			switch request.Action {
			case "start":
				if request.ID != "" || request.Command == "" || request.Lines != 0 {
					return nil, failure(pa.Invalid, "server_arguments")
				}
				spec := servers.Spec{Key: q.Bot.Key, Project: q.Bot.Project, Command: request.Command, Name: request.Name, ForgottenThrough: q.Binding.ForgottenThrough}
				resolved, err := h.serverProject(ctx, spec)
				if err != nil {
					return nil, err
				}
				spec.Directory, err = process.ResolveDirectory(resolved, request.Folder)
				if err != nil {
					return nil, err
				}
				if spec.Name == "" {
					name := []rune(strings.TrimSpace(spec.Command))
					if len(name) > pa.MaxNameRunes {
						name = name[:pa.MaxNameRunes]
					}
					spec.Name = string(name)
				}
				if err := spec.Validate(); err != nil {
					return nil, err
				}
				if err := journal(ctx, "server-start"); err != nil {
					return nil, err
				}
				saved, err := r.Start(context.WithValue(ctx, serverReviewKey{}, review), spec, h.o.ID())
				if err != nil {
					return nil, err
				}
				info := h.observedServerInfo(ctx, r, saved)
				result.Server = &info
			case "stop":
				if request.ID == "" || request.Command != "" || request.Name != "" || request.Folder != "" || request.Lines != 0 {
					return nil, failure(pa.Invalid, "server_arguments")
				}
				saved, err := find(ctx)
				if err != nil {
					return nil, err
				}
				if err := journal(ctx, "server-stop"); err != nil {
					return nil, err
				}
				if saved.Attempt.Identity.AttemptID != "" {
					if err := r.Stop(ctx, q.Bot.Key, q.Bot.Project, saved.ID, saved.Attempt.Identity.AttemptID); err != nil {
						return nil, err
					}
				}
				saved, err = find(ctx)
				if err != nil {
					return nil, err
				}
				info := h.observedServerInfo(ctx, r, saved)
				result.Server = &info
			case "list":
				if request.ID != "" || request.Key != "" || request.Command != "" || request.Name != "" || request.Folder != "" || request.Lines != 0 {
					return nil, failure(pa.Invalid, "server_arguments")
				}
				if err := journal(ctx, "server-list"); err != nil {
					return nil, err
				}
				rows, err := inspect(ctx)
				if err != nil {
					return nil, err
				}
				result.Servers = []app.DevServerInfo{}
				for _, saved := range rows {
					result.Servers = append(result.Servers, h.observedServerInfo(ctx, r, saved))
				}
			case "logs":
				if request.ID == "" || request.Key != "" || request.Command != "" || request.Name != "" || request.Folder != "" || request.Lines < 0 {
					return nil, failure(pa.Invalid, "server_arguments")
				}
				if err := journal(ctx, "server-logs"); err != nil {
					return nil, err
				}
				saved, err := find(ctx)
				if err != nil {
					return nil, err
				}
				if saved.Attempt.Identity.AttemptID == "" {
					result.Log = &app.DevServerLog{}
					break
				}
				log, err := r.ReadLog(ctx, q.Bot.Key, q.Bot.Project, saved.ID, saved.Attempt.Identity.AttemptID)
				if err != nil {
					return nil, err
				}
				text := log.Text
				if request.Lines > 0 {
					parts := strings.SplitAfter(text, "\n")
					if parts[len(parts)-1] == "" {
						parts = parts[:len(parts)-1]
					}
					if len(parts) > request.Lines {
						text = strings.Join(parts[len(parts)-request.Lines:], "")
						log.Truncated = true
					}
				}
				result.Log = &app.DevServerLog{Text: text, Truncated: log.Truncated}
			default:
				return nil, failure(pa.Unsupported, "server_action")
			}
			result, err := boundedServerResult(result)
			if err != nil {
				return nil, err
			}
			var out json.RawMessage
			err = h.serverMemory(ctx, servers.Spec{Key: q.Bot.Key, Project: q.Bot.Project, ForgottenThrough: q.Binding.ForgottenThrough}, func() error { var err error; out, err = json.Marshal(result); return err })
			return out, err
		}
		var raw json.RawMessage
		if request.Action == "start" || request.Action == "stop" {
			if request.Key == "" {
				return app.DevServerResult{}, failure(pa.Invalid, "server_command")
			}
			payload, _ := json.Marshal(struct {
				Action  string
				Request app.DevServerRequest
			}{request.Action, request})
			sum := sha256.Sum256(payload)
			raw, err = caller.Do(ctx, request.Key, hex.EncodeToString(sum[:]), run)
		} else {
			raw, err = run(ctx)
		}
		if err != nil {
			return app.DevServerResult{}, err
		}
		if err := check(ctx); err != nil {
			return app.DevServerResult{}, err
		}
		var result app.DevServerResult
		if err := json.Unmarshal(raw, &result); err != nil {
			return result, err
		}
		return result, nil
	}
	return nil
}

type hostBotDeleter struct{ h *Host }

func (d hostBotDeleter) DeleteBot(ctx context.Context, key pa.BotKey) error {
	d.h.mu.Lock()
	r := d.h.serverRegistry
	d.h.mu.Unlock()
	if r != nil {
		if err := r.RevokeBot(ctx, key, true); err != nil {
			return err
		}
	}
	return d.h.store.DeleteBot(ctx, key)
}
