package nextconfig

import "strings"

// ResolveProductLanguage applies the same script-first aliases as the browser
// resolver. Unknown and ambiguous Chinese tags safely choose English.
func ResolveProductLanguage(tag string) string {
	if !ValidProductLanguageTag(tag) {
		return "en"
	}
	parts := strings.Split(strings.ToLower(strings.ReplaceAll(tag, "_", "-")), "-")
	base := parts[0]
	switch base {
	case "zh":
		// A declared script decides before any region. An unfamiliar script
		// cannot safely be inferred from TW, CN, or another region.
		script := ""
		for _, part := range parts[1:] {
			if len(part) != 4 || !asciiLetters(part) {
				continue
			}
			if part != "hant" && part != "hans" || script != "" && script != part {
				return "en"
			}
			script = part
		}
		switch script {
		case "hant":
			return "zh-Hant"
		case "hans":
			return "zh-Hans"
		}
		for _, part := range parts[1:] {
			switch part {
			case "tw", "hk", "mo":
				return "zh-Hant"
			case "cn", "sg":
				return "zh-Hans"
			}
		}
		return "en"
	case "pt":
		return "pt-BR"
	case "en", "ja", "ko", "es", "fr", "de":
		return base
	}
	return "en"
}

// ValidProductLanguageTag accepts language tags and the old POSIX underscore
// spelling without relaxing the unrelated voice-language setting validator.
func ValidProductLanguageTag(tag string) bool {
	if len(tag) < 2 || len(tag) > 35 || strings.TrimSpace(tag) != tag {
		return false
	}
	parts := strings.Split(strings.ReplaceAll(tag, "_", "-"), "-")
	if len(parts[0]) < 2 || len(parts[0]) > 8 {
		return false
	}
	for i, part := range parts {
		if len(part) == 0 || len(part) > 8 {
			return false
		}
		for _, ch := range part {
			if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || i > 0 && ch >= '0' && ch <= '9' {
				continue
			}
			return false
		}
	}
	return true
}

func asciiLetters(part string) bool {
	for _, ch := range part {
		if ch < 'a' || ch > 'z' {
			return false
		}
	}
	return true
}
