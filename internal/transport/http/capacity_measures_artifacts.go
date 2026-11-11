package http

import (
	"github.com/sainteye/clawdline/internal/adapters/sessionfiles"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// artifactsCapacityMeasures measures every row of the register's "artifacts" area
// (capacity.AreaOf), one reading per row.
func (s *Server) artifactsCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		"artifacts.output_files": func() capacity.Reading {
			sessions, bytes, largest, files, path, err := sessionfiles.New(s.cfg.Dir).Usage()
			values := []int64{int64(sessions), bytes, largest, files, path}
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: values[3], Note: "durable output indexes; source file bytes are not copied into the store"}
		},
		"artifacts.output_index_bytes": func() capacity.Reading {
			sessions, bytes, largest, files, path, err := sessionfiles.New(s.cfg.Dir).Usage()
			values := []int64{int64(sessions), bytes, largest, files, path}
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: values[2], Note: "durable output indexes; source file bytes are not copied into the store"}
		},
		"artifacts.output_path_bytes": func() capacity.Reading {
			sessions, bytes, largest, files, path, err := sessionfiles.New(s.cfg.Dir).Usage()
			values := []int64{int64(sessions), bytes, largest, files, path}
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: values[4], Note: "durable output indexes; source file bytes are not copied into the store"}
		},
		"artifacts.output_sessions": func() capacity.Reading {
			sessions, bytes, largest, files, path, err := sessionfiles.New(s.cfg.Dir).Usage()
			values := []int64{int64(sessions), bytes, largest, files, path}
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: values[0], Note: "durable output indexes; source file bytes are not copied into the store"}
		},
		"artifacts.output_store_bytes": func() capacity.Reading {
			sessions, bytes, largest, files, path, err := sessionfiles.New(s.cfg.Dir).Usage()
			values := []int64{int64(sessions), bytes, largest, files, path}
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: values[1], Note: "durable output indexes; source file bytes are not copied into the store"}
		},
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
		"voice.vocabulary_terms": func() capacity.Reading {
			values, err := s.settingsFile().Read()
			if err != nil {
				return capacity.Unmeasured("the settings file cannot be read")
			}
			return capacity.Reading{Known: true, Used: int64(len(readVoiceOptions(values, true).custom))}
		},
		"voice.vocabulary_term_characters": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-term guard on a settings write"}
		},
		"voice.vocabulary_project_terms": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-dictation guard; project names are read only when ticked"}
		},
		"voice.polish_terms": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-cleanup guard; further terms are repaired locally"}
		},
		"voice.polish_characters": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-cleanup guard; a longer transcript is answered unchanged"}
		},
		"voice.polish_timeout_milliseconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-cleanup deadline; a slower turn is abandoned for Whisper's text"}
		},
		"voice.copilot_briefing_timeout_milliseconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-briefing deadline; a slower turn is abandoned and the copilot reads the Sessions itself"}
		},
		"voice.copilot_briefing_input_characters": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-briefing guard; a longer input is refused before a turn"}
		},
		"voice.copilot_briefing_characters": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-briefing guard; a longer answer is not handed to the call"}
		},
		"voice.copilot_briefing_reply_characters": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-briefing clip of each last reply, keeping both ends"}
		},
		"voice.copilot_key_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-settings-write guard on the pasted key"}
		},
		"voice.copilot_extra_instructions_characters": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-settings-write guard on the person's instructions"}
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
