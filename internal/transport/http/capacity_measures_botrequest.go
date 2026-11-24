package http

import (
	"context"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// botRequestCapacityMeasures measures every row of the register's
// "botrequest" area from the store's current values.
func (s *Server) botRequestCapacityMeasures() map[string]func() capacity.Reading {
	read := func(pick func(int64, int64, int64) capacity.Reading) func() capacity.Reading {
		return func() capacity.Reading {
			if s.store == nil {
				return capacity.Unmeasured("Bot request card store is unavailable")
			}
			o, err := s.store.BotRequestOccupancy(context.Background(), time.Now())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return pick(o.Cards, o.LiveReceipts, o.MergeHops)
		}
	}
	return map[string]func() capacity.Reading{
		"botrequest.page_rows": read(func(cards, _, _ int64) capacity.Reading {
			return capacity.Reading{Known: true, Used: min(cards, CapacityLimit("botrequest.page_rows")),
				Note: "rows the first page of the card list answers now; more come by cursor"}
		}),
		"botrequest.merge_hops": read(func(_, _, hops int64) capacity.Reading {
			return capacity.Reading{Known: true, Used: hops, Note: "longest stored merged_into walk"}
		}),
		"botrequest.receipts": read(func(_, receipts, _ int64) capacity.Reading {
			return capacity.Reading{Known: true, Used: receipts, Note: "receipts inside their one-day age"}
		}),
	}
}
