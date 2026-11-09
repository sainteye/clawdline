package main

import "testing"

func TestGrokCLIIdentityKeepsItsActualProvider(t *testing.T) {
	path := protocolIdentityFile(t, `{"assistant":"grok","conversation_id":"native-exact"}`)
	getenv := envOf(map[string]string{"CLAWDLINE_PROTOCOL_SESSION_FILE": path})
	id, name, err := conversationFromEnv(getenv)
	if err != nil || id != "native-exact" {
		t.Fatalf("identity=%q %v", id, err)
	}
	assistant, err := conversationAssistantFromEnv(getenv, name, id)
	if err != nil || assistant != "grok" {
		t.Fatalf("assistant=%q %v", assistant, err)
	}
	if _, err := conversationAssistantFromEnv(getenv, name, "another-native"); err == nil {
		t.Fatal("changed conversation address accepted")
	}
}
