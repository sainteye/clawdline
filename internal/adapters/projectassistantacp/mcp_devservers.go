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
		tool("start_dev_server", "Start a foreground development server owned by your Bot and Project. The Host keeps it after your turn ends. The command runs with the source user's files and network authority, not a shell sandbox; the source permission card/policy applies. Use the returned ID for controls. Reuse the exact key and arguments after an uncertain reply; do not retry with a new key.", map[string]any{"command": text(), "name": text(), "folder": text(), "idempotency_key": text()}, "command", "idempotency_key"),
		tool("list_dev_servers", "List only your Bot's current Project servers and observed states. Unknown is not stopped.", map[string]any{}),
		tool("stop_dev_server", "Stop your known server ID and its verified owned process tree. Reuse the exact key after an uncertain reply.", map[string]any{"id": text(), "idempotency_key": text()}, "id", "idempotency_key"),
		tool("dev_server_logs", "Read the latest bounded output tail for your known server ID. Optional lines selects a smaller tail; zero reads the available tail. Truncated also marks output lost across restart.", map[string]any{"id": text(), "lines": map[string]any{"type": "integer", "minimum": 0}}, "id"),
	}
}
