package http

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

func (s *Server) connectorRows(table string) capacity.Reading {
	if s.store == nil {
		return capacity.Unmeasured("connector store unavailable")
	}
	db := s.store.ProjectAssistantDatabase()
	var exists, used int64
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
		return capacity.Unmeasured("connector schema unreadable")
	}
	if exists == 0 {
		return capacity.Reading{Known: true, Note: "optional connector schema absent"}
	}
	// Table names are fixed by composition, never accepted from a request.
	if err := db.QueryRow(`SELECT coalesce(max(n),0) FROM (SELECT count(*) n FROM ` + table + ` GROUP BY source)`).Scan(&used); err != nil {
		return capacity.Unmeasured("connector source rows unreadable")
	}
	return capacity.Reading{Known: true, Used: used, Note: "largest actual count per source account and machine; unknown holds remain allocated"}
}

func (s *Server) extensionFilesMeasure(assets bool) capacity.Reading {
	root := filepath.Join(s.cfg.Dir, "project-assistants", "extension-executions")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return capacity.Reading{Known: true, Note: "no extension reservations"}
	}
	if err != nil {
		return capacity.Unmeasured("extension reservations unreadable")
	}
	if !assets {
		return capacity.Reading{Known: true, Used: int64(len(entries)), Note: "actual durable native asset and connector process reservations, including unknown cleanup"}
	}
	var largest int64
	for _, entry := range entries {
		var count int64
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return capacity.Unmeasured("unknown extension reservation kind")
		}
		err = filepath.WalkDir(filepath.Join(root, entry.Name()), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fs.ErrInvalid
			}
			if !d.IsDir() && d.Name() != "owner.json" {
				count++
			}
			return nil
		})
		if err != nil {
			return capacity.Unmeasured("materialized skill entries unreadable")
		}
		largest = max(largest, count)
	}
	return capacity.Reading{Known: true, Used: largest, Note: "largest actual materialized skill file count per execution; includes SKILL.md and binary assets"}
}
