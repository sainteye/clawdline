package projectassistant

import (
	"strings"
	"testing"
)

func TestCountsAsUnreadFollowsWhatWasAddressedToThePerson(t *testing.T) {
	self := BotKey{MachineID: "machine", BotID: "bot"}
	other := BotKey{MachineID: "machine", BotID: "peer"}
	toSelf := &Exchange{Target: ExchangeEndpoint{Key: self}}
	toOther := &Exchange{Target: ExchangeEndpoint{Key: other}}
	cases := []struct {
		name     string
		m        Message
		unread   bool
		activity bool
	}{
		{"reply to the person's message", Message{Kind: AssistantMessage, ForPerson: ForPersonFlag(true)}, true, false},
		{"routine or Bot-request reply", Message{Kind: AssistantMessage, ForPerson: ForPersonFlag(false)}, false, true},
		{"work result alone", Message{Kind: WorkMessage, ForPerson: ForPersonFlag(false)}, false, true},
		{"the person's own message", Message{Kind: UserMessage, ForPerson: ForPersonFlag(true)}, false, false},
		{"host notice", Message{Kind: NoticeMessage, ForPerson: ForPersonFlag(false)}, false, false},
		{"reply delivered to this Bot", Message{Kind: AssistantMessage, Exchange: toSelf, ForPerson: ForPersonFlag(true)}, false, false},
		{"notify_person while serving another Bot", Message{Kind: AssistantMessage, Exchange: toOther, ForPerson: ForPersonFlag(true)}, true, false},
		{"written before the field: reply", Message{Kind: AssistantMessage}, true, false},
		{"written before the field: work", Message{Kind: WorkMessage}, true, false},
		{"written before the field: notice", Message{Kind: NoticeMessage}, false, false},
	}
	for _, c := range cases {
		if got := CountsAsUnread(c.m, self); got != c.unread {
			t.Errorf("%s: unread %v, want %v", c.name, got, c.unread)
		}
		if got := NewActivity(c.m, self); got != c.activity {
			t.Errorf("%s: activity %v, want %v", c.name, got, c.activity)
		}
	}
}

func TestNotifyPersonTextIsOneSummaryLineAndOptionalDetail(t *testing.T) {
	text, err := NotifyPersonText(NotifyPersonRequest{Summary: " Deploy failed ", Detail: "The build is red.\nSee the log."})
	if err != nil || text != "Deploy failed\n\nThe build is red.\nSee the log." {
		t.Fatalf("text %q %v", text, err)
	}
	if NotifyPersonID(text) != NotifyPersonID(text) || NotifyPersonID(text) == NotifyPersonID("other") {
		t.Fatal("identity is not stable per text")
	}
	for name, q := range map[string]NotifyPersonRequest{
		"empty":     {Summary: "  "},
		"two lines": {Summary: "a\nb"},
		"long":      {Summary: strings.Repeat("a", MaxNotifySummaryBytes+1)},
		"detail":    {Summary: "a", Detail: strings.Repeat("b", MaxNotifyDetailBytes+1)},
	} {
		if _, err := NotifyPersonText(q); err == nil {
			t.Errorf("%s was admitted", name)
		}
	}
	if _, err := NotifyPersonText(NotifyPersonRequest{Summary: strings.Repeat("a", MaxNotifySummaryBytes), Detail: strings.Repeat("b", MaxNotifyDetailBytes)}); err != nil {
		t.Fatalf("the bounds themselves were refused: %v", err)
	}
}
