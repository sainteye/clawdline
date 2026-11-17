package projectassistant

// MemoryRow is a bounded preview; ID is a source-owned entry identifier, never a path.
type MemoryRow struct {
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

type MemoryView struct {
	Bot            Bot             `json:"bot"`
	ConversationID string          `json:"conversation_id"`
	Memory         MemoryViewState `json:"memory"`
	Layers         []MemoryLayer   `json:"layers"`
}
type MemoryDetail struct {
	Layer     string `json:"layer"`
	ID        string `json:"id"`
	Revision  string `json:"revision"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}
