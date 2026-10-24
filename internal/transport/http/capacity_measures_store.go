package http

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// storeCapacityMeasures measures every row of the register's "store" area
// (capacity.AreaOf), one reading per row.
func (s *Server) storeCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		capacity.StoreAntigravityProjectConsents: func() capacity.Reading {
			if s.store == nil {
				return capacity.Unmeasured("antigravity_consent_unreadable")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			n, err := s.store.AntigravityProjectConsentCount(ctx)
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: n}
		},
		capacity.StoreAntigravityConsentRootBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-command canonical Project root byte guard; no retained buffer"}
		},
		capacity.StoreAntigravityConsentIdentityBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-field actor, request key and operation identity byte guard; no retained buffer"}
		},
		// The store's read-only pool: connections in use now, and how often a
		// reader has waited for one since the daemon opened it.
		capacity.StoreReadConnections: func() capacity.Reading {
			if s.store == nil {
				return capacity.Reading{Known: true, Note: "this server has no store"}
			}
			pool := s.store.ReadPool()
			return capacity.Reading{Known: true, Used: int64(pool.InUse),
				Note: fmt.Sprintf("%d open; %d waits totalling %s", pool.OpenConnections,
					pool.WaitCount, pool.WaitDuration.Round(time.Millisecond))}
		},
		// The file's size and the disk's room, and this handle's own account
		// of the writes the database refused (limits N2).
		capacity.StoreDB: func() capacity.Reading { return s.store.Reading(s.cfg.Dir) },
		capacity.StoreEvents: func() capacity.Reading {
			n, err := s.store.EventCount(context.Background(), "")
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			removed, _ := s.store.EventPruneStats()
			return capacity.Reading{Known: true, Used: int64(n), Counters: capacity.Counters{Evicted: removed}}
		},
		capacity.StoreReceipts: func() capacity.Reading {
			uses, err := s.store.ReceiptUses(context.Background(), store.ReceiptWindow, time.Now())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			r := capacity.Reading{Known: true}
			tombstones := 0
			for _, u := range uses {
				if int64(u.Live) > r.Used {
					r.Used = int64(u.Live)
					r.Note = "fullest scope: " + u.Scope
				}
				tombstones += u.Tombstones
			}
			if tombstones > 0 {
				r.Note = strings.TrimPrefix(r.Note+"; ", "; ") + strconv.Itoa(tombstones) + " expired key(s) kept as tombstones"
			}
			return r
		},
		capacity.StoreDoneEffectRetention: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "an age the store prunes by on its maintenance pass; no retained buffer"}
		},
		capacity.StoreReceiptTombstoneRetention: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "an age the store prunes by on its maintenance pass; no retained buffer"}
		},
		capacity.StoreRetentionPruneBatch: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a per-pass ceiling of the store's maintenance prune; no retained buffer"}
		},
	}
}
