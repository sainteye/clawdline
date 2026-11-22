// Package userguide embeds user documentation that Bots can read offline.
package userguide

import _ "embed"

//go:embed bot-webhooks.md
var botWebhookEnglish string

//go:embed bot-webhooks.zh-TW.md
var botWebhookTraditionalChinese string

// BotWebhook returns the packaged Bot Webhook guide in the requested language.
func BotWebhook(lang string) string {
	if lang == "zh-Hant" {
		return botWebhookTraditionalChinese
	}
	return botWebhookEnglish
}
