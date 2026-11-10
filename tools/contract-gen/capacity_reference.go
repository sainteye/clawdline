package main

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// renderCapacityReference keeps the Agent's default-bound reference in the
// binary synchronized with the register. A running machine may lower a bound
// through an override; an Agent learns that from the typed refusal, because
// the live capacity view refuses an Agent's credential.
func renderCapacityReference(traditionalChinese bool) []byte {
	rows := capacity.Register()
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	var b bytes.Buffer
	if traditionalChinese {
		b.WriteString("# 此 build 的容量契約\n\n此表直接由 `internal/domain/capacity.Register()` 產生，列的是預設上限。執行時覆寫只能降低上限，所以預設值不代表還有餘額。Agent 動作前不需要先讀容量：直接送出請求，依它回的具型別拒絕處理。帶 `retry_after` 的回應是容量答覆，等那麼久再送同一筆請求；其他代碼見 `clawdline guide refusals`。`GET /v1/capacity`（需已配對裝置）與 `GET /v1/diagnostics`（需本機金鑰）是給人看即時狀態的；Agent 憑證在那裡被拒是正常的，不是停止工作的理由。\n\n| 名稱 | 預設上限 | 單位 | 達上限時 | 告知管道 | 偏離既定規則 |\n| --- | ---: | --- | --- | --- | --- |\n")
	} else {
		b.WriteString("# Capacity contract in this build\n\nGenerated directly from `internal/domain/capacity.Register()`; these are default bounds. A runtime override may only lower one, so a default bound is not available room. An Agent does not read capacity before acting: send the request and handle the typed refusal it answers. A `retry_after` is a capacity answer: wait that long, then send the same request; other codes are in `clawdline guide refusals`. `GET /v1/capacity` (a paired device) and `GET /v1/diagnostics` (the machine key) show live state to a person; an Agent's credential is refused there, and that refusal is not a reason to stop the work.\n\n| Name | Default bound | Unit | At limit | Told through | Current deviation |\n| --- | ---: | --- | --- | --- | --- |\n")
	}
	for _, row := range rows {
		channels := make([]string, len(row.Told))
		for i, channel := range row.Told {
			channels[i] = string(channel)
		}
		deviation := strings.ReplaceAll(row.Deviation, "|", "\\|")
		if deviation == "" {
			deviation = "—"
		}
		fmt.Fprintf(&b, "| `%s` | %d | `%s` | `%s` | `%s` | %s |\n",
			row.Name, row.Limit, row.Unit, row.AtLimit, strings.Join(channels, ", "), deviation)
	}
	return b.Bytes()
}
