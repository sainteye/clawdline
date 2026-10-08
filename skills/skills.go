// Package skills carries the agent guide and the skill stub inside the
// binary.
//
// A session learns how to use Clawdline from a guide, and a guide that is a
// file beside the app drifts from the build that answers its routes: the old
// app kept its guide in its bundle for exactly that reason. Compiled in, the
// guide `clawdline guide` prints is by construction the one written for the
// daemon it came with, on every platform, with nothing to find on disk.
//
// The sources are the Markdown files in skills/clawdline/. Edit them there;
// this package only embeds them.
package skills

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

//go:embed clawdline/SKILL.md clawdline/freshness.json clawdline/guide.md clawdline/guide.zh-TW.md clawdline/guide.ja.md clawdline/guide.zh-Hans.md clawdline/guide.ko.md clawdline/guide.es.md clawdline/guide.pt-BR.md clawdline/guide.fr.md clawdline/guide.de.md clawdline/routes.md clawdline/routes.zh-TW.md clawdline/capacity.md clawdline/capacity.zh-TW.md clawdline/refusals.md clawdline/refusals.zh-TW.md
var files embed.FS

// DefaultTopic is the guide `clawdline guide` prints with no topic.
const DefaultTopic = "en"

// topics maps the shipped language tags to their guides. English is the
// reference and the fallback for an unknown or unsupported language tag.
var topics = map[string]string{
	"en":      "clawdline/guide.md",
	"zh-Hant": "clawdline/guide.zh-TW.md",
	"ja":      "clawdline/guide.ja.md",
	"zh-Hans": "clawdline/guide.zh-Hans.md",
	"ko":      "clawdline/guide.ko.md",
	"es":      "clawdline/guide.es.md",
	"pt-BR":   "clawdline/guide.pt-BR.md",
	"fr":      "clawdline/guide.fr.md",
	"de":      "clawdline/guide.de.md",
}

// ErrUnknownTopic is kept for callers that still test it. Guide now falls
// back to English when a language tag is unknown.
var ErrUnknownTopic = errors.New("no such guide")

// Topics lists the shipped guides in product order, without compatibility aliases.
func Topics() []string {
	return []string{"en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"}
}

// ResolveTopic maps a language preference to a shipped guide. A script subtag
// takes precedence over a Chinese region; an ambiguous zh falls back to en.
func ResolveTopic(topic string) string {
	if topic == "" {
		return DefaultTopic
	}
	if len(topic) > 35 || strings.TrimSpace(topic) != topic {
		return DefaultTopic
	}
	topic = strings.ReplaceAll(topic, "_", "-")
	parts := strings.Split(topic, "-")
	for i, part := range parts {
		if len(part) == 0 || len(part) > 8 || i == 0 && len(part) < 2 {
			return DefaultTopic
		}
		for _, r := range part {
			if !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9') {
				return DefaultTopic
			}
		}
	}
	base := strings.ToLower(parts[0])
	switch base {
	case "zh":
		for _, part := range parts[1:] {
			if len(part) == 4 {
				switch strings.ToLower(part) {
				case "hant":
					return "zh-Hant"
				case "hans":
					return "zh-Hans"
				default:
					return DefaultTopic
				}
			}
		}
		for _, part := range parts[1:] {
			switch strings.ToLower(part) {
			case "tw", "hk", "mo":
				return "zh-Hant"
			case "cn", "sg":
				return "zh-Hans"
			}
		}
		return DefaultTopic
	case "pt":
		return "pt-BR"
	case "en", "ja", "ko", "es", "fr", "de":
		return base
	default:
		return DefaultTopic
	}
}

type guidePin struct {
	Source      string `json:"source"`
	Translation string `json:"translation"`
}

var (
	guidePinsOnce sync.Once
	guidePins     map[string]map[string]guidePin
)

func pins() map[string]map[string]guidePin {
	guidePinsOnce.Do(func() {
		data, err := files.ReadFile("clawdline/freshness.json")
		if err == nil {
			_ = json.Unmarshal(data, &guidePins)
		}
	})
	return guidePins
}

func pinned(pin guidePin, source, translation []byte) bool {
	return pin.Source == fmt.Sprintf("%x", sha256.Sum256(source)) &&
		pin.Translation == fmt.Sprintf("%x", sha256.Sum256(translation))
}

// effectiveGuide replaces stale translated parts with the current English
// source. Pins name both versions, so an edited source or damaged translation
// cannot silently teach an obsolete procedure. The intro is pinned too.
func effectiveGuide(topic string) ([]byte, []string, error) {
	lang := ResolveTopic(topic)
	english, err := files.ReadFile(topics[DefaultTopic])
	if err != nil || lang == DefaultTopic {
		return english, nil, err
	}
	translated, err := files.ReadFile(topics[lang])
	if err != nil {
		translated = nil
	}
	return mergeGuide(english, translated, lang, pins()[lang])
}

func mergeGuide(english, translated []byte, lang string, langPins map[string]guidePin) ([]byte, []string, error) {
	ep, es := split(english)
	tp, ts := split(translated)
	if len(es) != len(sections) {
		return nil, nil, fmt.Errorf("English guide has %d parts, want %d", len(es), len(sections))
	}
	pending := make([]string, 0, len(sections)+1)
	var out bytes.Buffer
	if pinned(langPins["intro"], ep, tp) {
		out.Write(tp)
	} else {
		out.Write(ep)
		pending = append(pending, "intro")
	}
	for i, section := range sections {
		var part []byte
		if i < len(ts) && len(ts) == len(es) && pinned(langPins[section.Name], es[i], ts[i]) {
			part = ts[i]
		} else {
			part = es[i]
			pending = append(pending, section.Name)
		}
		out.Write(part)
	}
	if len(pending) == 0 {
		return out.Bytes(), pending, nil
	}
	// Keep the notice in the preamble, so Core and Guide both show it without
	// creating an extra numbered part.
	body := out.Bytes()
	first := bytes.Index(body, []byte("\n## "))
	if first < 0 {
		return nil, nil, fmt.Errorf("guide %s has no first part", lang)
	}
	var noticed bytes.Buffer
	noticed.Write(body[:first+1])
	fmt.Fprintf(&noticed, "> %s: %s.\n\n", pendingNotice[lang], strings.Join(pending, ", "))
	noticed.Write(body[first+1:])
	return noticed.Bytes(), pending, nil
}

var pendingNotice = map[string]string{
	"zh-Hant": "以下部分待補譯，暫時顯示英文",
	"ja":      "次の部分は翻訳の更新待ちのため英語で表示します",
	"zh-Hans": "以下部分待补译，暂时显示英文",
	"ko":      "다음 부분은 번역 갱신 전까지 영어로 표시합니다",
	"es":      "Estas partes esperan traducción y se muestran en inglés",
	"pt-BR":   "Estas partes aguardam tradução e aparecem em inglês",
	"fr":      "Ces parties attendent une traduction et s'affichent en anglais",
	"de":      "Diese Abschnitte warten auf eine Übersetzung und erscheinen auf Englisch",
}

// PendingSections names translated parts that currently fall back to English.
// An empty result means every part is current with this build's English guide.
func PendingSections(topic string) ([]string, error) {
	_, pending, err := effectiveGuide(topic)
	return pending, err
}

// Guide is one guide's text. Empty and unknown tags use English. A stale
// translated part is replaced by English and listed in the preamble.
func Guide(topic string) ([]byte, error) {
	guide, _, err := effectiveGuide(topic)
	return guide, err
}

// Stub is the SKILL.md that `clawdline skill install` writes.
func Stub() []byte {
	data, err := files.ReadFile("clawdline/SKILL.md")
	if err != nil {
		// The file is embedded at build time; a build without it does not
		// compile, so this cannot happen at run time.
		panic(err)
	}
	return data
}

// RouteCatalog is the generated inventory compiled into this binary. It is
// discovery only; the current guide part remains the operation contract.
func RouteCatalog(topic string) ([]byte, error) {
	path := "clawdline/routes.md"
	if ResolveTopic(topic) == "zh-Hant" {
		path = "clawdline/routes.zh-TW.md"
	}
	return files.ReadFile(path)
}

// CapacityCatalog is the generated default-bound reference for this build.
func CapacityCatalog(topic string) ([]byte, error) {
	path := "clawdline/capacity.md"
	if ResolveTopic(topic) == "zh-Hant" {
		path = "clawdline/capacity.zh-TW.md"
	}
	return files.ReadFile(path)
}

// RefusalCatalog is the generated inventory of statically declared codes.
func RefusalCatalog(topic string) ([]byte, error) {
	path := "clawdline/refusals.md"
	if ResolveTopic(topic) == "zh-Hant" {
		path = "clawdline/refusals.zh-TW.md"
	}
	return files.ReadFile(path)
}
