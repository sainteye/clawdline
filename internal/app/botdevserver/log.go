package botdevserver

import (
	"bytes"
	"sync"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

type Log struct {
	Text      string
	Bytes     int
	Truncated bool
}

// LogTail never blocks on a subscriber or retains the complete output. One
// instance belongs to one run; restarting creates a different instance.
type LogTail struct {
	mu      sync.Mutex
	tail    []byte
	dropped bool
}

func (l *LogTail) incomplete() {
	l.mu.Lock()
	l.dropped = true
	l.mu.Unlock()
}

func (l *LogTail) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	limit := capacity.MaximumBotServerLogBytes
	if len(b) >= limit {
		l.dropped = l.dropped || len(l.tail)+len(b) > limit
		l.tail = append(l.tail[:0], b[len(b)-limit:]...)
	} else {
		if extra := len(l.tail) + len(b) - limit; extra > 0 {
			copy(l.tail, l.tail[extra:])
			l.tail = l.tail[:len(l.tail)-extra]
			l.dropped = true
		}
		l.tail = append(l.tail, b...)
	}
	return len(b), nil
}

func (l *LogTail) Snapshot() Log {
	l.mu.Lock()
	defer l.mu.Unlock()
	text := bytes.ToValidUTF8(l.tail, []byte("\uFFFD"))
	clipped := l.dropped
	if len(text) > capacity.MaximumBotServerLogBytes {
		text = text[len(text)-capacity.MaximumBotServerLogBytes:]
		for len(text) > 0 && !utf8.RuneStart(text[0]) {
			text = text[1:]
		}
		clipped = true
	}
	return Log{Text: string(text), Bytes: len(l.tail), Truncated: clipped}
}
