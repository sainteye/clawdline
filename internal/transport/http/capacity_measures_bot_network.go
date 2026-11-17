package http

import (
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"time"
)

func (s *Server) botCloudMeasure(kind string) func() capacity.Reading {
	return func() capacity.Reading {
		if kind == "receipts" {
			if s.store == nil {
				return capacity.Unmeasured("Bot Cloud store unavailable")
			}
			db := s.store.ProjectAssistantDatabase()
			var exists, used int64
			if e := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='project_assistant_network_cloud_receipts'`).Scan(&exists); e != nil {
				return capacity.Unmeasured("Bot Cloud receipt table unreadable")
			}
			if exists == 0 {
				return capacity.Reading{Known: true, Note: "optional Bot Cloud transport store absent"}
			}
			if e := db.QueryRow(`SELECT coalesce(max(n),0) FROM (SELECT count(*) n FROM project_assistant_network_cloud_receipts GROUP BY machine)`).Scan(&used); e != nil {
				return capacity.Unmeasured("Bot Cloud transport receipts unreadable")
			}
			return capacity.Reading{Known: true, Used: used, Note: "retained send and target transport correlation; never native execution"}
		}
		s.projectAssistants.mu.RLock()
		n := s.projectAssistants.network
		s.projectAssistants.mu.RUnlock()
		if n == nil {
			return capacity.Reading{Known: true, Note: "Bot Cloud source unconfigured"}
		}
		n.mu.Lock()
		rail, lease, next := n.rail, n.lease, n.nextRefresh
		n.mu.Unlock()
		var used int64
		var refused int64
		switch kind {
		case "pending", "frames":
			if rail != nil {
				frames, pending, count := rail.Usage()
				if kind == "frames" {
					refused = count
				}
				used = int64(frames)
				if kind == "pending" {
					used = int64(pending)
				}
			}
		case "endpoints":
			used = int64(len(lease.Endpoints))
		case "lease":
			used = max(0, lease.ExpiresAt-time.Now().Unix())
		case "refresh":
			used = max(0, int64(time.Until(next).Seconds()))
		}
		return capacity.Reading{Known: true, Used: used, Counters: capacity.Counters{Refused: refused}, Note: "current dedicated Bot source occupancy; no viewer or Session queue"}
	}
}

func (s *Server) botNetworkGrantMeasure() capacity.Reading {
	if s.store == nil {
		return capacity.Unmeasured("Bot network source store is unavailable")
	}
	db := s.store.ProjectAssistantDatabase()
	var exists, used int64
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='project_assistant_network_grants'`).Scan(&exists); err != nil {
		return capacity.Unmeasured("Bot network grant table cannot be inspected")
	}
	if exists == 0 {
		return capacity.Reading{Known: true, Note: "optional Bot network grant table is absent; no retained rows"}
	}
	if err := db.QueryRow(`SELECT coalesce(max(n),0) FROM (SELECT count(*) n FROM project_assistant_network_grants GROUP BY machine)`).Scan(&used); err != nil {
		return capacity.Unmeasured("Bot network grants cannot be read")
	}
	return capacity.Reading{Known: true, Used: used, Note: "largest retained Bot network grant count per source machine, including revoked identities"}
}

func (s *Server) botAssistantRowsMeasure(table string) func() capacity.Reading {
	return func() capacity.Reading {
		if s.store == nil {
			return capacity.Unmeasured("assistant source store is unavailable")
		}
		db := s.store.ProjectAssistantDatabase()
		var exists, used int64
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
			return capacity.Unmeasured("assistant source table cannot be inspected")
		}
		if exists == 0 {
			return capacity.Reading{Known: true, Note: "optional assistant source table is absent; no retained rows"}
		}
		var query string
		switch table {
		case "project_assistant_gateway_grants":
			query = `SELECT coalesce(max(n),0) FROM (SELECT count(*) n FROM project_assistant_gateway_grants GROUP BY account,machine)`
		case "project_assistant_gateway_receipts":
			query = `SELECT coalesce(max(n),0) FROM (SELECT count(*) n FROM project_assistant_gateway_receipts GROUP BY account,machine)`
		default:
			return capacity.Unmeasured("unsupported assistant source table")
		}
		if err := db.QueryRow(query).Scan(&used); err != nil {
			return capacity.Unmeasured("assistant source rows cannot be read")
		}
		return capacity.Reading{Known: true, Used: used, Note: "largest retained assistant namespace per account and source machine, including revoked or unknown records"}
	}
}

// Read the same retained namespace counted at admission. Terminal and unknown
// effects remain occupancy; unavailable storage is never an invented zero.
func (s *Server) botNetworkReceiptMeasure() capacity.Reading {
	if s.store == nil {
		return capacity.Unmeasured("Bot network source store is unavailable")
	}
	db := s.store.ProjectAssistantDatabase()
	var exists, used int64
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='project_assistant_network_receipts'`).Scan(&exists); err != nil {
		return capacity.Unmeasured("Bot network receipt table cannot be inspected")
	}
	if exists == 0 {
		return capacity.Reading{Known: true, Note: "optional Bot network receipt table is absent; no retained rows"}
	}
	if err := db.QueryRow(`SELECT coalesce(max(n),0) FROM (SELECT count(*) n FROM project_assistant_network_receipts GROUP BY machine)`).Scan(&used); err != nil {
		return capacity.Unmeasured("Bot network receipts cannot be read")
	}
	return capacity.Reading{Known: true, Used: used, Note: "largest retained Bot network receipt count per source machine, including terminal and unknown effects"}
}
