package orchestrator

import (
	"strings"
	"testing"
)

func TestADraftTitleNamesTheResultRatherThanTheObservation(t *testing.T) {
	b, _ := newTestBroker(t)
	project := t.TempDir()
	id := "7e110000-0000-4000-8000-000000000001"
	bad := []string{
		"使用者問了三次『現在到底是什麼狀況』，而沒有一頁在答這個問題",
		"還在照舊 app 的規則走的地方，要改成照現在的",
		"`activity` 只影響排序，沒有畫在列上",
		"剛開的 Codex 認不出來：沒有 resume 就沒有身分",
		"修正 session 列表顯示問題",
	}
	for _, title := range bad {
		writeBrief(t, b, id, project, map[string]any{"title": title})
		_, err := b.ReadDraft(id)
		if refusalCode(err) != "bad_task" || !strings.Contains(refusalMessage(err), "title") {
			t.Errorf("%q: %v", title, err)
		}
	}
	writeBrief(t, b, id, project, map[string]any{"title": "Session 列會顯示最後活動時間"})
	if _, err := b.ReadDraft(id); err != nil {
		t.Fatalf("outcome title: %v", err)
	}
}
