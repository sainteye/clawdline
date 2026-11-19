package projectassistantacp

// Advertised only when this native execution has the source-owned service.
// There is no approval, environment, identity, PID or arbitrary stop argument.
func mcpDevServerTools() []any {
	text := func() any { return map[string]any{"type": "string", "minLength": 1} }
	tool := func(name, description string, properties map[string]any, required ...string) any {
		if required == nil {
			required = []string{}
		}
		return map[string]any{"name": name, "description": description, "inputSchema": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}}
	}
	return []any{
		tool("start_dev_server", "Start a foreground development server owned by your Bot and Project. The Host keeps it after your turn ends. The command runs with the source user's files and network authority, not a shell sandbox; the source permission card/policy applies. Use the returned ID for controls. Reuse the exact key and arguments after an uncertain reply; do not retry with a new key. 啟動由本 Bot 與 Project 擁有的前景開發伺服器，回合結束後仍由 Host 維持。命令使用來源使用者的檔案與網路權限，並非終端沙箱；依來源批准卡與政策執行。後續以回傳 ID 操作，結果不明沿用相同鍵與參數。", map[string]any{"command": text(), "name": text(), "folder": text(), "idempotency_key": text()}, "command", "idempotency_key"),
		tool("list_dev_servers", "List only your Bot's current Project servers and observed states. Unknown is not stopped. 列出本 Bot 目前 Project 的伺服器與觀察狀態；未知不等於已停止。", map[string]any{}),
		tool("stop_dev_server", "Stop your known server ID and its verified owned process tree. Reuse the exact key after an uncertain reply. 停止既有伺服器 ID 與經核對的自有程序樹；結果不明沿用相同鍵。", map[string]any{"id": text(), "idempotency_key": text()}, "id", "idempotency_key"),
		tool("dev_server_logs", "Read the latest bounded output tail for your known server ID. Optional lines selects a smaller tail; zero reads the available tail. Truncated also marks output lost across restart. 讀取既有伺服器 ID 的有上限輸出尾段；lines 可縮小行數，0 讀取可用尾段。truncated 也表示重啟後有輸出遺失。", map[string]any{"id": text(), "lines": map[string]any{"type": "integer", "minimum": 0}}, "id"),
	}
}
