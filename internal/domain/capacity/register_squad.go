package capacity

// squadBounds is every registered row named squad.*, personas.*, and workflow.*.
func squadBounds() []Entry {
	return []Entry{
		{
			// Retained launch identities preserve original workflow bindings.
			// New launches are refused instead of evicting resume evidence.
			Name: WorkflowLaunches, Class: Evidence, Unit: Rows,
			Limit: 4096, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender, Health}, EvictedBy: Person,
			Sources: []string{"internal/adapters/store.MaxWorkflowLaunches"},
		},
		{
			// One complete workflow snapshot, including tool choices.
			Name: WorkflowSnapshotBytes, Class: Evidence, Unit: Bytes,
			Limit: 1 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender, Health}, EvictedBy: Person,
			Sources: []string{"internal/adapters/store.MaxWorkflowSnapshotBytes"},
		},
		{
			// The personas compiled into this daemon (docs/personas.md). The
			// catalog is embedded, so it cannot grow while the daemon runs: a
			// catalog past the limit does not load, and the persona package's
			// own test refuses it before a build ships. Nothing is let go.
			Name: PersonasCatalog, Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/domain/persona.MaxPersonas"},
		},
		{
			// One persona's injected text — the precedence preamble, the body
			// and its source line — which a session reads on every turn. A
			// longer one is refused at load in the same way.
			Name: PersonasTextBytes, Class: Buffer, Unit: Bytes,
			Limit: 8 << 10, AtLimit: Refuse,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/domain/persona.MaxPersonaBytes"},
		},
		{
			// One complete immutable AI squad launch document. A larger
			// launch is refused before an intent or snapshot is persisted.
			Name: SquadSnapshotBytes, Class: Evidence, Unit: Bytes,
			Limit: 1 << 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/store.MaxSquadSnapshotBytes"},
		},
		{
			// Console refuses another enabled skill before the complete launch
			// document can approach its larger, authoritative snapshot limit.
			Name: SquadConsoleActiveSkillBytes, Class: Buffer, Unit: Bytes,
			Limit: 512 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Person,
			Sources: []string{"internal/domain/squad.MaxSquadConsoleActiveSkillBytes"},
		},
		{
			// One recovery query reads this many pending launch intents. Later
			// pages continue from a cursor instead of refusing new Sessions.
			Name: SquadRecoveryRows, Class: Buffer, Unit: Rows,
			Limit: 256, AtLimit: Refuse,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/store.MaxSquadRecoveryRows"},
		},
		{
			// An agent's idempotency key for one reported skill event.
			Name: SquadEventIDBytes, Class: Buffer, Unit: Bytes,
			Limit: 128, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/store.MaxSquadEventIDBytes"},
		},
		{
			// One JSON skill-event report from a local session.
			Name: SquadEventBodyBytes, Class: Buffer, Unit: Bytes,
			Limit: 4 << 10, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/http.maxSquadEventBodyBytes"},
		},
		{
			// A durable skill-event cursor page; more events remain for
			// the next read rather than being dropped from the store.
			Name: SquadEventPageRows, Class: Buffer, Unit: Rows,
			Limit: 100, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/store.MaxSquadEventPageRows"},
		},
		{
			// One explicitly requested Board-role classification reads the
			// closed catalog and as much of the item's title and description as
			// fits beside it. The rest of a long description is not sent.
			Name: PersonaSuggestionContextBytes, Class: Observation, Unit: Bytes,
			Limit: 16 << 10, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/http.personaSuggestionContextLimit"},
		},
		{
			Name: SquadEntities, Class: Evidence, Unit: Rows,
			Limit: 1024, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Health, Sender}, EvictedBy: Person,
			Sources: []string{"internal/domain/squad.MaxSquadEntities"},
		},
		{
			Name: SquadSettingsRows, Class: Evidence, Unit: Rows,
			Limit: 10000, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Health, Sender}, EvictedBy: Person,
			Sources: []string{"internal/domain/squad.MaxSquadSettingsRows"},
		},
		{
			Name: SquadReceipts, Class: Idempotency, Unit: Rows,
			Limit: 10000, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Person,
			Sources: []string{"internal/domain/squad.MaxSquadReceipts"},
		},
		{
			Name: SquadBodyBytes, Class: Buffer, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/squad.MaxSquadBodyBytes"},
		},
		{
			Name: SkillSourceSummaryRunes, Class: Observation, Unit: Characters,
			Limit: 240, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.maxSkillSourceSummaryRunes"},
		},
		{
			Name: SquadLaunchSkillWhenRunes, Class: Observation, Unit: Characters,
			Limit: 240, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/squadfiles.maxSkillWhenRunes"},
		},
		{
			Name: SquadRequestBytes, Class: Buffer, Unit: Bytes,
			Limit: 256 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/squad.MaxSquadRequestBytes"},
		},
		{
			Name: SquadPlaceLookup, Class: Observation, Unit: Rows,
			Limit: 1024, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/squad.MaxSquadPlaceLookup"},
		},
	}
}
