package cloudops

func init() {
	for _, word := range []string{"routines", "routine-runs"} {
		register(op{name: "project-assistants." + word, read: true, shape: projectAssistantReply, partial: projectAssistantFailure})
	}
	for _, word := range []string{"routine-save", "routine-enabled", "routine-delete", "routine-test", "routine-hook"} {
		register(op{name: "project-assistants." + word, guard: projectAssistantGuard, shape: projectAssistantReply, partial: projectAssistantFailure})
	}
}
