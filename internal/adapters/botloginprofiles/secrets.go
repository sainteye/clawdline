package botloginprofiles

import (
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"time"
)

type secretTarget struct {
	key                pa.BotKey
	fence              pa.Fence
	profile, authority string
	revision           int64
	agent              pa.Agent
	expires            time.Time
}

func (t secretTarget) matches(q pa.LoginProfileAction) bool {
	return t.key == q.Command.Key && t.fence == q.Fence && t.profile == q.ProfileID && t.revision == q.Revision && t.authority == q.Authority && t.agent == q.Agent
}
func (r *Registry) aad(p pa.LoginProfile) []byte {
	return []byte("clawdline.bot-profile-key.v1\x00" + r.source + "\x00" + string(p.Agent) + "\x00" + p.ID)
}

// SecretTargetUsage counts current handoff addresses without exposing identities.
func (r *Registry) SecretTargetUsage() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, target := range r.targets {
		if !r.Now().Before(target.expires) {
			delete(r.targets, id)
		}
	}
	return len(r.targets)
}
