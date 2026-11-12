package http

import "github.com/sainteye/clawdline/internal/domain/capacity"

func (s *Server) projectAssistantCapacityMeasures() map[string]func() capacity.Reading {
	// This build contains validators but installs no Bot runtime or queues.
	// Zero means no allocation in this contract-only build, not native ready.
	// PA-05 must replace this with real observations when it installs the
	// module; an installed runtime that cannot report remains Unmeasured.
	unallocated := func() capacity.Reading {
		return capacity.Reading{Known: true, Note: "contract only; Bot runtime and queues are not installed or allocated"}
	}
	return map[string]func() capacity.Reading{
		capacity.BotsNameRunes:        unallocated,
		capacity.BotsInstructionBytes: unallocated,
		capacity.BotsMessageBytes:     unallocated,
		capacity.BotsPendingTurns:     unallocated,
		capacity.BotsPageRows:         unallocated,
		capacity.BotsEventRows:        unallocated,
	}
}
