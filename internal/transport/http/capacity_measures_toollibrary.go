package http

import (
	"context"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

func (s *Server) toolLibraryCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		capacity.ToolLibraryProjects: func() capacity.Reading {
			if s.store == nil {
				return capacity.Reading{Known: true, Note: "no store attached"}
			}
			state, err := s.store.ToolLibrary(context.Background())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: int64(len(state.Saved))}
		},
		capacity.ToolLibraryProjectBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-project record refused before persistence"}
		},
		capacity.ToolLibraryRequestBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-operation request, not retained as a live buffer"}
		},
		capacity.ToolLibraryRequestKeyBytes: func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-operation retry key"} },
	}
}
