package cloud

import (
	"encoding/json"

	"github.com/sainteye/clawdline/internal/domain/terminal"
)

type cloudHistoryResult struct {
	Lines        []string `json:"lines"`
	Truncated    bool     `json:"truncated"`
	OmittedLines int      `json:"omitted_lines"`
}

// boundedCloudHistory retains complete newest lines while fitting the entire
// receipt, including request metadata, inside the Cloud reply budget.
func boundedCloudHistory(lines []string, req terminalRequest) (cloudHistoryResult, error) {
	if lines == nil {
		lines = []string{}
	}
	if len(lines) > terminal.MaxHistoryLines {
		return cloudHistoryResult{}, terminal.Refuse(terminal.CodeHistoryTooLarge, "history has too many lines")
	}
	total := 0
	for _, line := range lines {
		if len(line) > CloudTerminalHistoryLineBytesLimit {
			return cloudHistoryResult{}, terminal.Refuse(terminal.CodeHistoryLineTooLarge, "one history line exceeds the Cloud limit")
		}
		total += len(line) + 1
		if total > CloudTerminalHistoryCaptureBytesLimit {
			return cloudHistoryResult{}, terminal.Refuse(terminal.CodeHistoryTooLarge, "history capture exceeds the Cloud byte limit")
		}
	}
	base := terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
		Connection: req.Connection, Operation: req.Operation, TerminalID: req.TerminalID, Status: "ok"}
	result := cloudHistoryResult{Lines: []string{}}
	for start := len(lines); start >= 0; start-- {
		result.Lines = lines[start:]
		result.OmittedLines = start
		result.Truncated = start > 0
		base.Result = result
		data, err := json.Marshal(base)
		if err != nil {
			return cloudHistoryResult{}, err
		}
		if len(data) > CloudTerminalHistoryReceiptBytesLimit {
			if start == len(lines) {
				return cloudHistoryResult{}, terminal.Refuse(terminal.CodeHistoryTooLarge, "history receipt metadata exceeds the Cloud limit")
			}
			result.Lines = lines[start+1:]
			result.OmittedLines = start + 1
			result.Truncated = result.OmittedLines > 0
			return result, nil
		}
	}
	return result, nil
}
