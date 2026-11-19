package projectassistant

import "unicode/utf8"

type HandoffRef struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
}

type HandoffCardState string

const (
	HandoffDraft     HandoffCardState = "draft"
	HandoffKept      HandoffCardState = "kept"
	HandoffAdopted   HandoffCardState = "adopted"
	HandoffWithdrawn HandoffCardState = "withdrawn"
)

// HandoffCard lives on the source. It is separate from the conversation log,
// archived Bot aggregates and command receipts.
type HandoffCard struct {
	Ref                 HandoffRef       `json:"ref"`
	Fence               Fence            `json:"fence"`
	MemoryRevision      int64            `json:"memory_revision"`
	InstructionRevision int64            `json:"instruction_revision"`
	BookmarkMessageID   string           `json:"bookmark_message_id,omitempty"`
	State               HandoffCardState `json:"state"`
	Text                string           `json:"text,omitempty"`
	TurnID              string           `json:"turn_id,omitempty"`
	Claimed             bool             `json:"claimed,omitempty"`
}

func ValidateHandoffRef(ref HandoffRef) error {
	if ref.ID == "" || !utf8.ValidString(ref.ID) || ref.Revision < 1 {
		return fail(Invalid, "handoff_ref")
	}
	return nil
}

func ValidateHandoffText(text string) error {
	if !utf8.ValidString(text) {
		return fail(Invalid, "handoff_text")
	}
	if len(text) > MaxMessageBytes {
		return fail(CapacityFull, "handoff_text")
	}
	return nil
}
