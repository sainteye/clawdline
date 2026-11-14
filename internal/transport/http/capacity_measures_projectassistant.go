package http

import (
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

func (s *Server) projectAssistantCapacityMeasures() map[string]func() capacity.Reading {
	usage := func() (app.RuntimeUsage, bool) {
		p := &s.projectAssistants
		p.mu.RLock()
		h, failed := p.host, p.inspectionFailed
		p.mu.RUnlock()
		if h == nil {
			return app.RuntimeUsage{Known: !failed}, failed
		}
		_, err := h.Service()
		u := h.Usage()
		u.Known = u.Known && !failed
		return u, err == nil
	}
	count := func(selectCount func(app.RuntimeUsage) int, note string) func() capacity.Reading {
		return func() capacity.Reading {
			u, installed := usage()
			if !installed {
				return capacity.Reading{Known: true, Note: "source unconfigured or Bot schema never installed; no runtime allocation"}
			}
			return capacity.Reading{Known: u.Known, Used: int64(selectCount(u)), Note: note}
		}
	}
	sessions := func(selectCount func(app.SessionUsage) int, note string) func() capacity.Reading {
		return count(func(u app.RuntimeUsage) int {
			// Each register bound applies to one native session. Expose its
			// largest observed occupancy, not an invented aggregate RPC count.
			n := 0
			for _, session := range u.Sessions {
				n = max(n, selectCount(session))
			}
			return n
		}, note)
	}
	pure := func() capacity.Reading {
		return capacity.Reading{Known: true, Note: "effect-free input validator or request-owned bounded page; no retained allocation"}
	}
	unobserved := func() capacity.Reading {
		_, installed := usage()
		return capacity.Reading{Known: !installed, Note: "no allocation before installation; installed durable queues, native descendants and IO duration are not exposed by Host.Usage"}
	}
	return map[string]func() capacity.Reading{
		capacity.BotsOwnedDescendants:      unobserved,
		capacity.BotsReadViewCursorBytes:   pure,
		capacity.BotsNameRunes:             pure,
		capacity.BotsInstructionBytes:      pure,
		capacity.BotsMessageBytes:          pure,
		capacity.BotsPendingTurns:          unobserved,
		capacity.BotsPageRows:              pure,
		capacity.BotsEventRows:             pure,
		capacity.BotsActors:                count(func(u app.RuntimeUsage) int { return u.ActiveActors }, "actual Host.Usage actors; pending or unknown cleanup is not known zero"),
		capacity.BotsProcesses:             count(func(u app.RuntimeUsage) int { return u.Processes }, "actual Host.Usage process slots, including unresolved owners; not native readiness"),
		capacity.BotsRPCPending:            sessions(func(u app.SessionUsage) int { return u.PendingRPC }, "largest shared ACP request/owned MCP connection occupancy; no provider is configured in production"),
		capacity.BotsPermissions:           sessions(func(u app.SessionUsage) int { return u.PendingPermissions }, "largest session-reported pending permission occupancy"),
		capacity.BotsNotifications:         sessions(func(u app.SessionUsage) int { return u.QueuedNotifications }, "largest session-reported native notification queue"),
		capacity.BotsFrames:                unobserved,
		capacity.BotsCapture:               sessions(func(u app.SessionUsage) int { return u.CapturedBytes }, "largest session-reported captured byte count"),
		capacity.BotsStderr:                sessions(func(u app.SessionUsage) int { return u.StderrBytes }, "largest session-reported stderr byte count; no content is exposed"),
		capacity.BotsWrite:                 unobserved,
		capacity.BotsCleanup:               unobserved,
		capacity.BotsRPCDeadline:           unobserved,
		capacity.BotsPrompt:                unobserved,
		capacity.BotsSurvey:                pure,
		capacity.BotsChatGap:               pure,
		capacity.BotsEventRefresh:          pure,
		capacity.BotsDispatchSeconds:       pure,
		capacity.BotsAdapterInstallSeconds: pure,
		capacity.BotsToolProbe:             pure,
		capacity.BotsToolProbeBytes:        pure,
		capacity.BotsFeatureProbeBytes:     pure,
	}
}
