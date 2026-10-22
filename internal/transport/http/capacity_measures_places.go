package http

import (
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// placesCapacityMeasures measures every row of the register's "places" area
// (capacity.AreaOf), one reading per row.
func (s *Server) placesCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		"places.antigravity_probe_output_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-probe output guard; no retained buffer"}
		},
		"places.antigravity_probe_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-probe deadline and pipe-drain guard; no retained buffer"}
		},
		"places.git_config_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-read guard; no retained buffer"}
		},
		"places.repo_lookups": func() capacity.Reading {
			entries, _ := s.projectReaders().repos.Counts()
			return capacity.Reading{Known: true, Used: entries}
		},
		"places.repo_readers": func() capacity.Reading {
			_, readers := s.projectReaders().repos.Counts()
			return capacity.Reading{Known: true, Used: readers}
		},
		"places.repo_wait_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-new-lookup wait; no retained buffer"}
		},
		"places.setup_lookups": func() capacity.Reading {
			entries, _ := s.projectReaders().setups.Counts()
			return capacity.Reading{Known: true, Used: entries}
		},
		"places.setup_readers": func() capacity.Reading {
			_, readers := s.projectReaders().setups.Counts()
			return capacity.Reading{Known: true, Used: readers}
		},
		"places.setup_wait_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-new-lookup wait; no retained buffer"}
		},
		"places.setup_age_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "completed setup cache refresh age"}
		},
		capacity.PlacesRegistered: func() capacity.Reading {
			rows, err := s.projectReaders().registry.List()
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			hidden, err := s.projectReaders().registry.Hidden()
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: int64(len(rows) + len(hidden))}
		},
	}
}
