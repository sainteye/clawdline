package capacity

// daemonBounds is every registered row named audit.*, deploy.*, devices.*,
// diagnostics.*, log.*, notification.*, push.*, settings.* and sse.*: the
// daemon's own plumbing: audit, logs, devices, pushes, settings and streams.
func daemonBounds() []Entry {
	return append([]Entry{
		{
			// remote-audit.jsonl (limits N12). Rotated into segments at this
			// size; a segment is never deleted by the daemon.
			Name: AuditSecurity, Class: SecurityAudit, Unit: Bytes,
			Limit: 8 << 20, AtLimit: Rotate,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
		},
		{
			// CLAWDLINE_NEXT_DIR/logs/daemon.log (limits N29). At this size it
			// becomes a segment and a new file is begun; the daemon keeps the
			// newest of those segments and deletes the rest, so the whole log
			// is at most the current file and maxSegments more.
			Name: LogDaemon, Class: DiagnosticLog, Unit: Bytes,
			Limit: 10 << 20, AtLimit: Rotate,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/logs.maxSegments"},
		},
		{
			// remote.json's paired devices (limits N13). Each is somebody's
			// access, so only a person lets one go: at the limit a new
			// pairing, password sign-in or browser device is refused with
			// 507 device_list_full, and revoking still works. The read bound
			// is registered here too: the most rows this limit allows,
			// written at their longest, stay far under it (a test holds
			// that), so the file cannot grow past what the daemon reads at
			// startup — the limit speaks first.
			Name: DevicesList, Class: Evidence, Unit: Rows,
			Limit: 512, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Projects:  true,
			Sources:   []string{"internal/adapters/devices.storeLimit"},
		},
		{
			// push/subscriptions.json (limits N14). A person's standing
			// request to be told, one row per device: at the limit a new
			// subscription is refused with 507 subscriptions_full rather
			// than one being dropped, and the read bound is registered for
			// the same reason as the device list's.
			Name: PushSubscriptions, Class: Evidence, Unit: Rows,
			Limit: 128, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Projects:  true,
			Sources:   []string{"internal/adapters/push.subscriptionsLimit"},
		},
		{
			// The screens one event stream has been told moved and has not
			// yet written out (limits N19). Each screen holds one frame, its
			// newest: a newer revision replaces a waiting one and is counted
			// as coalesced, so a slow stream is told late and never told
			// wrong. Past this many different screens waiting on one stream
			// the stream is ended, counted as disconnected, and the page's
			// reconnect reads every screen afresh. Used is the most any one
			// stream has waiting now.
			Name: SSEScreenPending, Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Disconnect,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
		},
		{
			// One settings change, whether it came from this machine's own
			// console or the narrow two-model Cloud route. Past the bound the
			// request is refused before it is copied into config.json.
			Name: SettingsRequestBodyBytes, Class: Buffer, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.settingsRequestBodyLimit",
				"internal/app/cloudops.defaultModelsCloudBodyLimit"},
		},
		{
			// A person's custom waiting conditions in config.json. A new
			// rule is refused once the list is full; existing rules remain.
			Name: NotificationRulesRows, Class: Evidence, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Health, Sender}, EvictedBy: Person,
			Sources: []string{"internal/domain/notificationrule.MaximumRules"},
		},
		{
			Name: NotificationRuleLabel, Class: Buffer, Unit: Characters,
			Limit: 80, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/notificationrule.MaximumLabelRunes"},
		},
		{
			Name: NotificationRuleWait, Class: Observation, Unit: Seconds,
			Limit: 120 * 60, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/notificationrule.MaxMinutes"},
		},
		{
			// The unprivileged Linux deploy keeps the previous release selected
			// until the daemon and its console prove the new commit. A full wait
			// restores that previous selector and restarts it.
			Name: DeployHealthSeconds, Class: Buffer, Unit: Seconds,
			Limit: 30, AtLimit: Refuse,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/domain/capacity.DeployHealthLimit"},
		},
		{
			// The route shapes /v1/diagnostics.routes counts. A shape past
			// the limit coalesces into the one `overflow` count; the call is
			// answered as before, and only its row is missing.
			Name: DiagnosticsRouteStatKeys, Class: Buffer, Unit: Rows,
			Limit: 512, AtLimit: Coalesce,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.MaxRouteStatKeys"},
		},
		{
			// The recent durations each counted route keeps for its p50 and
			// p90; the oldest is overwritten first. `max` is kept apart.
			Name: DiagnosticsRouteLatencySamples, Class: Observation, Unit: Rows,
			Limit: 256, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.MaxRouteLatencySamples"},
		},
	}, apiPerformanceBounds()...)
}
