package projectassistant

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
	"unicode/utf16"
)

type roomMentionCases struct {
	Members []struct{ ID, Name string } `json:"members"`
	Cases   []struct {
		Name   string      `json:"name"`
		Author string      `json:"author"`
		Text   string      `json:"text"`
		Marks  [][2]string `json:"marks"`
		Asked  []string    `json:"asked"`
	} `json:"cases"`
}

// The room's marks and its routing are one reading: each mark the viewer is
// shown names exactly the members RoomNamedBy asks.
func TestRoomMentionsMarkWhoTheRoomAsks(t *testing.T) {
	raw, err := os.ReadFile("testdata/room-mentions.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture roomMentionCases
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	var members []RoomSpeaker
	for _, m := range fixture.Members {
		members = append(members, RoomSpeaker{Key: BotKey{MachineID: "m", BotID: m.ID}, Name: m.Name})
	}
	for _, c := range fixture.Cases {
		t.Run(c.Name, func(t *testing.T) {
			var author BotKey
			if c.Author != "" {
				author = BotKey{MachineID: "m", BotID: c.Author}
			}
			found := RoomMentions(members, author, c.Text)
			units := utf16.Encode([]rune(c.Text))
			var marks [][2]string
			marked := map[string]bool{}
			last := -1
			for _, m := range found {
				who := m.Member.BotID
				if m.Everyone {
					who = "*"
					for _, s := range members {
						if s.Key != author {
							marked[s.Key.BotID] = true
						}
					}
				} else {
					marked[who] = true
				}
				if m.Start >= last { // longest first at each start; shown once
					marks = append(marks, [2]string{string(utf16.Decode(units[m.Start:m.End])), who})
					last = m.End
				}
			}
			if marks == nil {
				marks = [][2]string{}
			}
			if !slices.Equal(marks, c.Marks) {
				t.Errorf("marks = %q, want %q", marks, c.Marks)
			}
			var asked, shown []string
			for _, s := range RoomNamedBy(members, author, c.Text) {
				asked = append(asked, s.Key.BotID)
			}
			for id := range marked {
				shown = append(shown, id)
			}
			slices.Sort(asked)
			slices.Sort(shown)
			if !slices.Equal(asked, shown) || !slices.Equal(asked, c.Asked) {
				t.Errorf("asked %v, marked %v, want %v", asked, shown, c.Asked)
			}
		})
	}
}
