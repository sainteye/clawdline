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
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"
)

//go:embed clawdline/SKILL.md clawdline/guide.md clawdline/guide.zh-TW.md
var files embed.FS

// DefaultTopic is the guide `clawdline guide` prints with no topic.
const DefaultTopic = "en"

// topics maps a topic to its file. The English guide is the reference; the
// Traditional Chinese one is its translation for the person reading along.
var topics = map[string]string{
	"en":    "clawdline/guide.md",
	"zh-TW": "clawdline/guide.zh-TW.md",
}

// ErrUnknownTopic is a topic this build does not carry.
var ErrUnknownTopic = errors.New("no such guide")

// Topics lists the guides this build carries, in a fixed order.
func Topics() []string {
	out := make([]string, 0, len(topics))
	for t := range topics {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Guide is one guide's text. An empty topic is DefaultTopic.
func Guide(topic string) ([]byte, error) {
	if topic == "" {
		topic = DefaultTopic
	}
	name, ok := topics[topic]
	if !ok {
		return nil, fmt.Errorf("%w named %q; this build carries %s", ErrUnknownTopic, topic,
			strings.Join(Topics(), ", "))
	}
	return files.ReadFile(name)
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
