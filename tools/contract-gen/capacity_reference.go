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
// through an override, so the live capacity view wins for the next action.
func renderCapacityReference(traditionalChinese bool) []byte {
	rows := capacity.Register()
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	var b bytes.Buffer
	if traditionalChinese {
		b.WriteString("# 此 build 的容量契約\n\n此表直接由 `internal/domain/capacity.Register()` 產生，列的是預設上限。執行時覆寫只能降低上限；操作前請讀目標機器的即時容量狀態。缺少讀數或容量已滿時，依目前指南與具型別拒絕停止或處理，勿把預設值當成可用餘額。`GET /v1/capacity` 需要已配對裝置；`GET /v1/diagnostics` 需要本機金鑰。\n\n| 名稱 | 預設上限 | 單位 | 達上限時 | 告知管道 | 偏離既定規則 |\n| --- | ---: | --- | --- | --- | --- |\n")
	} else {
		b.WriteString("# Capacity contract in this build\n\nGenerated directly from `internal/domain/capacity.Register()`; these are default bounds. A runtime override may only lower one. Read the target machine's live capacity state before an action. If a reading is missing or capacity is full, follow the current guide and typed refusal; a default bound is not available room. `GET /v1/capacity` needs a paired device; `GET /v1/diagnostics` needs the machine key.\n\n| Name | Default bound | Unit | At limit | Told through | Current deviation |\n| --- | ---: | --- | --- | --- | --- |\n")
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
