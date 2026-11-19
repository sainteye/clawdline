package projectassistant

// MemoryRow is a bounded preview; ID is a source-owned entry identifier, never a path.
type MemoryRow struct {
	Kind      string         `json:"kind,omitempty"`
	Title     string         `json:"title,omitempty"`
	ID        string         `json:"id"`
	Text      string         `json:"text"`
	Author    string         `json:"author"`
	UpdatedAt string         `json:"updated_at"`
	Sources   []MemorySource `json:"sources"`
}
type MemoryLayer struct {
	Layer     string      `json:"layer"`
	Revision  string      `json:"revision"`
	Used      int         `json:"used"`
	Cap       int         `json:"cap"`
	Available bool        `json:"available"`
	Truncated bool        `json:"truncated"`
	Entries   []MemoryRow `json:"entries"`
}
type MemoryViewState struct {
	Key              BotKey       `json:"key"`
	Project          ProjectScope `json:"project"`
	Revision         int64        `json:"revision"`
	ForgottenThrough int64        `json:"forgotten_through"`
	ForgottenCount   int64        `json:"forgotten_count"`
}

// MemoryDreamHistory contains previews from completed runs within the authorized
// Bot scope. A source must apply its current retention rules before exposing them.
type MemoryDreamHistory struct {
	Runs []MemoryDreamRun `json:"runs"`
}

type MemoryDreamRun struct {
	ID       string   `json:"id"`
	At       string   `json:"at"`
	Added    []string `json:"added"`
	Archived []string `json:"archived"`
	Notes    []string `json:"notes"`
	Undone   bool     `json:"undone,omitempty"`
}

type MemoryView struct {
	Bot            Bot                 `json:"bot"`
	ConversationID string              `json:"conversation_id"`
	Memory         MemoryViewState     `json:"memory"`
	Layers         []MemoryLayer       `json:"layers"`
	Dreams         *MemoryDreamHistory `json:"dreams,omitempty"`
}
type MemoryDetail struct {
	Kind      string `json:"kind,omitempty"`
	Title     string `json:"title,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Layer     string `json:"layer"`
	ID        string `json:"id"`
	Revision  string `json:"revision"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}
