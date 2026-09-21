package app

import (
	"fmt"
	"strings"
)

type scheduleNoticeKind string

const (
	scheduleNoticeInvalidJSON        scheduleNoticeKind = "invalid_json"
	scheduleNoticeInvalidSchema      scheduleNoticeKind = "invalid_schema"
	scheduleNoticeProjectUnavailable scheduleNoticeKind = "project_unavailable"
	scheduleNoticeMissed             scheduleNoticeKind = "missed"
	scheduleNoticeRefused            scheduleNoticeKind = "refused"
)

type scheduleLanguage interface {
	DisplayLanguage() string
}

func (b *ScheduleBook) notificationLanguage() string {
	if source, ok := b.Broker.(scheduleLanguage); ok {
		if language := source.DisplayLanguage(); language != "" {
			return language
		}
	}
	// This daemon ships Traditional Chinese as its built-in catalog. A book
	// read apart from the daemon has no second language source to ask.
	return "zh-Hant"
}

func invalidScheduleNoticeKind(kind string) scheduleNoticeKind {
	switch kind {
	case "unreadable_json":
		return scheduleNoticeInvalidJSON
	case "project_unavailable":
		return scheduleNoticeProjectUnavailable
	default:
		return scheduleNoticeInvalidSchema
	}
}

// scheduleNotice is the daemon-side catalog for schedule pushes. PushSend's
// boundary is already rendered title/body text, so the receiver has no stable
// code-and-arguments envelope to localize. Producer detail therefore never
// enters this function; only a classified kind and its safe parameter do.
func scheduleNotice(language string, kind scheduleNoticeKind, value string) string {
	traditional := strings.HasPrefix(strings.ToLower(strings.ReplaceAll(language, "_", "-")), "zh")
	if traditional {
		switch kind {
		case scheduleNoticeInvalidJSON:
			return fmt.Sprintf("無法讀取排程檔 %s，已停用。", value)
		case scheduleNoticeProjectUnavailable:
			return fmt.Sprintf("排程檔 %s 指定的專案資料夾目前無法使用，已停用。", value)
		case scheduleNoticeMissed:
			return "排程執行已超過可補跑的時間，這次沒有啟動。"
		case scheduleNoticeRefused:
			return fmt.Sprintf("排程這次無法啟動（%s）。", value)
		default:
			return fmt.Sprintf("排程檔 %s 的內容不符合格式，已停用。", value)
		}
	}
	switch kind {
	case scheduleNoticeInvalidJSON:
		return fmt.Sprintf("Schedule file %s could not be read and was disabled.", value)
	case scheduleNoticeProjectUnavailable:
		return fmt.Sprintf("Schedule file %s names a project directory that is currently unavailable and was disabled.", value)
	case scheduleNoticeMissed:
		return "The scheduled run missed its catch-up window and did not start."
	case scheduleNoticeRefused:
		return fmt.Sprintf("The scheduled run could not start (%s).", value)
	default:
		return fmt.Sprintf("Schedule file %s has an invalid format and was disabled.", value)
	}
}
