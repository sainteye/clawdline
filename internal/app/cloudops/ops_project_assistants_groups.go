package cloudops

func init() {
	for _, word := range []string{"groups", "group-history", "team-plans"} {
		register(op{name: "project-assistants." + word, read: true, shape: projectAssistantReply, partial: projectAssistantFailure})
	}
	for _, word := range []string{"group-create", "group-edit", "group-delete", "group-stop", "group-send", "team-settings", "team-answer"} {
		register(op{name: "project-assistants." + word, guard: projectAssistantGroupGuard, shape: projectAssistantReply, partial: projectAssistantFailure})
	}
}

func projectAssistantGroupGuard(p plan) *Refusal {
	if refusal := projectAssistantGuard(p); refusal != nil {
		return refusal
	}
	b, err := decodeBody(p.local.Body)
	if err != nil {
		return projectAssistantMalformed()
	}
	key := b.genAt("command.key").(map[string]any)
	if members, ok := b.genAt("settings.members").([]any); ok {
		for _, member := range members {
			if member.(map[string]any)["machine_id"] != key["machine_id"] {
				return projectAssistantMalformed()
			}
		}
	}
	if reply, ok := b.genAt("reply_to_author").(map[string]any); ok && reply["machine_id"] != key["machine_id"] {
		return projectAssistantMalformed()
	}
	return nil
}
