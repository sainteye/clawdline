package orchestrator

import (
	"sync"
	"time"
)

// MaxBotViewingSeconds is how long one "I am looking at this chat" report from
// a console counts. The console repeats it well inside this while the chat is
// on screen and in focus, and says so at once when it stops.
const MaxBotViewingSeconds = 45

// MaxBotViewingHoldSeconds is the longest a decision push waits because the
// person seems to be looking at its chat. Past it the push goes anyway, so a
// screen left open on a desk delays the notification, never loses it.
const MaxBotViewingHoldSeconds = 300

// MaxBotViewingChats is how many chats this daemon remembers as being looked at.
// A report past it is ignored: the push is sent, never held.
const MaxBotViewingChats = 256

// BotPresence remembers, in memory only, which Bot chats a console reported
// as on screen. A restart forgets everything, which only means a push is not
// held; it is never a reason to drop one.
type BotPresence struct {
	mu   sync.Mutex
	seen map[botChat]time.Time
}

type botChat struct{ bot, conversation string }

// Report records that a console is (viewing) or is no longer looking at the chat.
func (p *BotPresence) Report(bot, conversation string, viewing bool, at time.Time) {
	if p == nil || bot == "" || conversation == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	key := botChat{bot, conversation}
	if !viewing {
		delete(p.seen, key)
		return
	}
	if p.seen == nil {
		p.seen = map[botChat]time.Time{}
	}
	if _, known := p.seen[key]; !known && len(p.seen) >= MaxBotViewingChats {
		for k, last := range p.seen {
			if at.Sub(last) >= MaxBotViewingSeconds*time.Second {
				delete(p.seen, k)
			}
		}
		if len(p.seen) >= MaxBotViewingChats {
			return
		}
	}
	p.seen[key] = at
}

// Viewing is whether some console reported the chat on screen within MaxBotViewingSeconds.
func (p *BotPresence) Viewing(bot, conversation string, at time.Time) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	last, ok := p.seen[botChat{bot, conversation}]
	return ok && at.Sub(last) < MaxBotViewingSeconds*time.Second
}

// Count is how many chats are remembered, fresh or not yet pruned.
func (p *BotPresence) Count() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.seen)
}
