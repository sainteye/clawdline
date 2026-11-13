package browserbridge

const (
	SetupRequired      Refusal = "browser_setup_required"
	NativeTools        Refusal = "browser_native_tools"
	SessionBusy        Refusal = "browser_session_busy"
	SessionChoicesFull Refusal = "browser_session_limit"
)

func (b *Bridge) Mode() string { b.mu.Lock(); defer b.mu.Unlock(); return b.mode }

func (b *Bridge) SessionBusy(conversation string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for tab, owner := range b.owner {
		if owner != conversation {
			continue
		}
		if b.closing[tab] != "" {
			return Closing
		}
		if b.person[tab] {
			return PersonControlling
		}
	}
	for _, owner := range b.inflight {
		if owner == conversation {
			return SessionBusy
		}
	}
	return nil
}

func (b *Bridge) OwnerOf(tab int) string { b.mu.Lock(); defer b.mu.Unlock(); return b.owner[tab] }

func (b *Bridge) knowsResult(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.waiting[id]; ok {
		return true
	}
	for _, closing := range b.closing {
		if closing == id {
			return true
		}
	}
	for _, row := range b.audit {
		if row.ID == id {
			return true
		}
	}
	return false
}

// pauseProfile is startup-only. Do not rewrite unknown or corrupt persisted
// bytes merely to fail closed while opening the profile router.
func (b *Bridge) pauseProfile() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stopped = true
	b.stopEpoch++
	b.signal()
}

func (b *Bridge) capacityOccupancy() (int, int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.queue), len(b.audit), len(b.thumbs)
}
