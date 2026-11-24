package capacity

import br "github.com/sainteye/clawdline/internal/domain/botrequest"

// botRequestBounds is every row named botrequest.*: the Bot request card's
// limits (docs/bot-request-cards.md §10). Each refuses at its limit with a
// typed bot_request_* refusal in the answer to the Bot or person writing;
// nothing already recorded is let go to make room.
func botRequestBounds() []Entry {
	row := func(name string, unit Unit, limit int64, source string) Entry {
		return Entry{Name: name, Class: Buffer, Unit: unit, Limit: limit, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/botrequest." + source}}
	}
	return []Entry{
		row("botrequest.open_cards", Rows, br.MaxOpenCards, "MaxOpenCards"),
		row("botrequest.description_bytes", Bytes, br.MaxDescriptionBytes, "MaxDescriptionBytes"),
		row("botrequest.events_per_card", Rows, br.MaxEventsPerCard, "MaxEventsPerCard"),
		row("botrequest.verdicts_per_card", Rows, br.MaxVerdictsPerCard, "MaxVerdictsPerCard"),
		row("botrequest.checklist_entries", Rows, br.MaxChecklistEntries, "MaxChecklistEntries"),
		row("botrequest.note_bytes", Bytes, br.MaxNoteBytes, "MaxNoteBytes"),
		row("botrequest.label_bytes", Bytes, br.MaxLabelBytes, "MaxLabelBytes"),
		row("botrequest.chain_bots", Rows, br.MaxChainBots, "MaxChainBots"),
		row("botrequest.page_rows", Rows, br.MaxPageRows, "MaxPageRows"),
		row("botrequest.batch_cards", Rows, br.MaxBatchCards, "MaxBatchCards"),
		row("botrequest.merge_hops", Rows, br.MaxMergeHops, "MaxMergeHops"),
		{
			// The card writes' (actor, idempotency key) receipts. They expire
			// by time, like board.receipts; a full window refuses a new write
			// rather than evicting a receipt somebody may still retry against.
			Name: "botrequest.receipts", Class: Idempotency, Unit: Rows,
			Limit: br.MaxReceipts, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Notice}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/botrequest.MaxReceipts"},
		},
	}
}
