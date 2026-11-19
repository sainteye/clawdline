package projectassistant

import (
	"context"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// DevServerTools is additive and bound by the source to one native execution.
// Arguments never name a Bot, Project, environment, process or permission.
type DevServerTools interface {
	DevServersAvailable(context.Context) error
	DevServers(context.Context, DevServerRequest) (DevServerResult, error)
	// PublishDevServers fences one bounded transport write, not an Agent wait.
	PublishDevServers(context.Context, func() error) error
}

type DevServerRequest struct {
	Action  string `json:"-"`
	Key     string `json:"idempotency_key,omitempty"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Command string `json:"command,omitempty"`
	Folder  string `json:"folder,omitempty"`
	Lines   int    `json:"lines,omitempty"`
}

type DevServerInfo struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Command     string          `json:"command"`
	Folder      string          `json:"folder"`
	Attempt     string          `json:"attempt"`
	State       string          `json:"state"`
	Failure     string          `json:"failure,omitempty"`
	Fingerprint string          `json:"fingerprint"`
	Revision    int64           `json:"revision"`
	PortsKnown  bool            `json:"ports_known"`
	Ports       []DevServerPort `json:"ports"`
}

type DevServerPort struct {
	Port   int      `json:"port"`
	Bind   string   `json:"bind"`
	Local  string   `json:"local"`
	Remote []string `json:"remote"`
}

// Human controls resolve only a previously displayed ID. There is no command,
// directory, environment or model-supplied principal in this request.
type DevServerControl struct {
	Key            pa.BotKey `json:"key"`
	ID             string    `json:"id"`
	IdempotencyKey string    `json:"idempotency_key"`
	Fingerprint    string    `json:"fingerprint"`
	Attempt        string    `json:"attempt"`
	Revision       int64     `json:"revision"`
	Action         string    `json:"-"`
}

type HumanDevServers interface {
	Read(context.Context, pa.BotKey, pa.ProjectScope, string) (DevServerResult, error)
	Control(context.Context, pa.BotKey, pa.ProjectScope, HumanServerAuthority, DevServerControl) (DevServerResult, error)
}

func (s *Service) DevServers(ctx context.Context, key pa.BotKey, id string) (DevServerResult, error) {
	if s.opts.DevServers == nil {
		return DevServerResult{}, serviceFailure(pa.Unsupported, "dev_servers")
	}
	if _, err := s.authorize(ctx, key, false); err != nil {
		return DevServerResult{}, err
	}
	bot, err := s.opts.Repository.Bot(ctx, key)
	if err != nil {
		return DevServerResult{}, err
	}
	return s.opts.DevServers.Read(ctx, key, bot.Project, id)
}

func (s *Service) ControlDevServer(ctx context.Context, q DevServerControl) (DevServerResult, error) {
	if s.opts.DevServers == nil {
		return DevServerResult{}, serviceFailure(pa.Unsupported, "dev_servers")
	}
	a, err := s.authorize(ctx, q.Key, true)
	if err != nil {
		return DevServerResult{}, err
	}
	if q.ID == "" || q.IdempotencyKey == "" || q.Revision < 0 {
		return DevServerResult{}, serviceFailure(pa.Invalid, "server_control")
	}
	bot, err := s.opts.Repository.Bot(ctx, q.Key)
	if err != nil {
		return DevServerResult{}, err
	}
	if _, err := s.authorizeEffect(ctx, q.Key, true, a); err != nil {
		return DevServerResult{}, err
	}
	return s.opts.DevServers.Control(ctx, q.Key, bot.Project, HumanServerAuthority{Principal: a.PrincipalID, Check: func(ctx context.Context) error { _, err := s.authorizeEffect(ctx, q.Key, true, a); return err }}, q)
}

type DevServerLog struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

type DevServerResult struct {
	Servers []DevServerInfo `json:"servers"`
	Server  *DevServerInfo  `json:"server,omitempty"`
	Log     *DevServerLog   `json:"log,omitempty"`
}

// HumanServerAuthority is source composition only, never a JSON credential.
type HumanServerAuthority struct {
	Principal string
	Check     func(context.Context) error
}
