package projectassistant

import "testing"

func TestPeerDispatchNoticeIsWrittenInOneProductLanguage(t *testing.T) {
	cases := []struct{ language, requester, title, want string }{
		{"zh-Hant", "Atlas｜產品開發主管", "Fix the header", "應 Atlas 的請求派出任務「Fix the header」。"},
		{"zh-TW", "", "Fix", "應 另一個 Bot 的請求派出任務「Fix」。"},
		{"en", " Atlas ｜Product lead", "Fix the header", "Dispatched task “Fix the header” at Atlas's request."},
		{"en", "", "", "Dispatched task “-” at another Bot's request."},
		{"", "Atlas", "Fix", "Dispatched task “Fix” at Atlas's request."},
		{"xx-unsupported", "Atlas", "Fix", "Dispatched task “Fix” at Atlas's request."},
		{"zh-Hant", "｜Lead", "Fix", "應 另一個 Bot 的請求派出任務「Fix」。"},
	}
	for _, c := range cases {
		if got := PeerDispatchNoticeText(c.language, c.requester, c.title); got != c.want {
			t.Errorf("PeerDispatchNoticeText(%q, %q, %q) = %q, want %q", c.language, c.requester, c.title, got, c.want)
		}
	}
}
