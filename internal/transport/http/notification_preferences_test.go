package http

import (
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
)

func TestNotificationPreferencesKeepLegacyChoicesAndSeparateEvents(t *testing.T) {
	s := pushServer(t)
	file := nextconfig.Open(s.cfg.Dir)
	if !s.notificationEnabled("push_on_model_capacity") {
		t.Fatal("model capacity default is off")
	}
	if _, err := file.Set(map[string]any{"push_on_model_capacity": false}); err != nil {
		t.Fatal(err)
	}
	if s.notificationEnabled("push_on_model_capacity") {
		t.Fatal("model capacity ignores explicit false")
	}
	if s.notificationEnabled("push_on_app_release_success") || s.notificationEnabled("push_on_cloud_release_success") || s.notificationEnabled("push_on_release_failure") {
		t.Fatal("a fresh installation unexpectedly enables release pushes")
	}
	if !s.notificationEnabled("push_on_waiting") || !s.notificationEnabled("push_on_board_done") {
		t.Fatal("a fresh installation unexpectedly disables existing notices")
	}
	if _, err := file.Set(map[string]any{"push_on_deploy": true, "orchestrator_agent_notify": false}); err != nil {
		t.Fatal(err)
	}
	if !s.notificationEnabled("push_on_app_release_success") || !s.notificationEnabled("push_on_cloud_release_success") || !s.notificationEnabled("push_on_release_failure") || s.notificationEnabled("push_on_waiting") {
		t.Fatal("new controls did not inherit stored choices")
	}
	if _, err := file.Set(map[string]any{"push_on_app_release_success": false, "push_on_release_failure": false, "push_on_waiting": true, "push_on_board_done": false}); err != nil {
		t.Fatal(err)
	}
	if s.notificationEnabled("push_on_app_release_success") || !s.notificationEnabled("push_on_cloud_release_success") || s.notificationEnabled("push_on_release_failure") ||
		!s.notificationEnabled("push_on_waiting") || s.notificationEnabled("push_on_board_done") {
		t.Fatal("one event switch changed another event")
	}
}
