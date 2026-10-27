package http

import (
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// artifactsCapacityMeasures measures every row of the register's "artifacts" area
// (capacity.AreaOf), one reading per row.
func (s *Server) artifactsCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		"artifacts.video_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-file guard; browser Blob released on navigation"}
		},
		"artifacts.video_chunk_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-read guard; one sequential chunk per viewer"}
		},
		"artifacts.report_total_image_pixels": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-report cumulative decoded pixel guard; later images beyond the budget are omitted"}
		},
		"artifacts.report_image_side": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-image decoded side guard; invalid report images are omitted"}
		},
		"artifacts.report_image_pixels": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-image decoded pixel guard; invalid report images are omitted"}
		},
		"icons.saved": func() capacity.Reading {
			if s.icons == nil {
				return capacity.Reading{Known: true, Note: "no icon registry attached"}
			}
			return capacity.Reading{Known: true, Used: s.icons.SavedCount()}
		},
		"icons.side":          func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-icon dimension guard"} },
		"icons.request_bytes": func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-request byte guard"} },
		capacity.ScreensCaptureSlots: func() capacity.Reading {
			if s.inventory.Held == nil {
				return capacity.Reading{Known: true, Note: "this inventory takes no screen captures"}
			}
			_, slots := s.inventory.Held.Reading()
			return slots
		},
		capacity.VoiceModelDownloadBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-download guard; the temporary file is released after verification"}
		},
		capacity.ArtifactsImages: func() capacity.Reading {
			if s.pictures.store == nil {
				return capacity.Unmeasured("this server keeps no picture store")
			}
			count, _ := s.pictures.store.Readings(time.Now())
			return count
		},
		capacity.ArtifactsImageSize: func() capacity.Reading {
			if s.pictures.store == nil {
				return capacity.Unmeasured("this server keeps no picture store")
			}
			_, bytes := s.pictures.store.Readings(time.Now())
			return bytes
		},
		capacity.ArtifactsDrops: func() capacity.Reading {
			if s.pictures.drops == nil {
				return capacity.Unmeasured("this server keeps no drop cache")
			}
			return s.pictures.drops.Reading()
		},
		capacity.ArtifactsDropsYoung: func() capacity.Reading {
			if s.pictures.drops == nil {
				return capacity.Unmeasured("this server keeps no drop cache")
			}
			return s.pictures.drops.YoungReading()
		},
	}
}
