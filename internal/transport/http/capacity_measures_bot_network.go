package http

import "github.com/sainteye/clawdline/internal/domain/capacity"

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
