package projectassistant

import "context"

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
	ID      string `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
	Folder  string `json:"folder"`
	Attempt string `json:"attempt"`
	State   string `json:"state"`
	Failure string `json:"failure,omitempty"`
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
