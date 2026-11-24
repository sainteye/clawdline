package capacity

import br "github.com/sainteye/clawdline/internal/domain/botrequest"

// botRequestBounds is every row named botrequest.* (docs/bot-request-cards.md
// §10). A card is a few kilobytes of text, so its sizes and counts are
// deliberately not bounded here: the request body limit every route has is the
// only size guard, and a cap that refused ordinary work would hurt more than
// the rows it saves.
func botRequestBounds() []Entry {
	return []Entry{
		{
			// One page of a card listing; more rows come by cursor, so a
			// larger request is shortened, never refused.
			Name: "botrequest.page_rows", Class: Buffer, Unit: Rows,
			Limit: br.MaxPageRows, AtLimit: Coalesce,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/botrequest.MaxPageRows"},
		},
		{
			// A loop guard on merged_into: past it a dispatch naming the
			// card is refused rather than followed forever.
			Name: "botrequest.merge_hops", Class: Buffer, Unit: Rows,
			Limit: br.MaxMergeHops, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/botrequest.MaxMergeHops"},
		},
		{
			// The card writes' (actor, idempotency key) receipts. They expire
			// after a day; when the window is full the oldest expire early,
			// so a full window never refuses a write.
			Name: "botrequest.receipts", Class: Idempotency, Unit: Rows,
			Limit: br.MaxReceipts, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/botrequest.MaxReceipts"},
		},
	}
}
