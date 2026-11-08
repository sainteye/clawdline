package capacity

// releaseBounds is every registered row named release.*.
func releaseBounds() []Entry {
	return []Entry{
		{
			// One verification reads only its manifest and signature list in
			// parallel. It admits no additional metadata work or retry queue.
			Name: ReleaseMetadataConcurrency, Class: Buffer, Unit: Rows,
			Limit: 2, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.MetadataReadConcurrencyLimit"},
		},
		{
			// One release manifest.json. A longer one is refused as
			// manifest_malformed and nothing of it is followed.
			Name: ReleaseManifestBytes, Class: Buffer, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release.maxManifestBytes"},
		},
		{
			// One manifest.sig.json, the list of signatures over the
			// manifest. A longer one is refused as manifest_signature_invalid.
			Name: ReleaseSignatureBytes, Class: Buffer, Unit: Bytes,
			Limit: 16 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release.maxSignatureBytes"},
		},
		{
			// The signed manifest of a release install's channel, as the release
			// check last read it. At this age (plus up to the jitter) it is read
			// again; the old answer is replaced only by a good new one, or dropped
			// when the release is gone (404).
			Name: ReleaseCheckIntervalSeconds, Class: Observation, Unit: Seconds,
			Limit: 6 * 3600, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.CheckIntervalSecondsLimit"},
		},
		{
			// The most added at random to the release check's period, so machines
			// started together do not ask together.
			Name: ReleaseCheckJitterSeconds, Class: Observation, Unit: Seconds,
			Limit: 1800, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.CheckJitterSecondsLimit"},
		},
		{
			// One read of a release manifest, its signatures or the release list. At
			// the limit the read is abandoned and its error shown beside the last
			// good answer.
			Name: ReleaseFetchTimeoutSeconds, Class: Observation, Unit: Seconds,
			Limit: 30, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.FetchTimeoutSecondsLimit"},
		},
		{
			// One artifact's download. At the limit the download is abandoned, the
			// partial file removed, and the update recorded failed with
			// download_failed; the running release is untouched.
			Name: ReleaseDownloadTimeoutSeconds, Class: Buffer, Unit: Seconds,
			Limit: 900, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.DownloadTimeoutSecondsLimit"},
		},
		{
			// One release artifact. A manifest naming a larger one is refused before
			// anything is downloaded.
			Name: ReleaseArtifactBytes, Class: Buffer, Unit: Bytes,
			Limit: 512 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.maxArtifactBytes"},
		},
		{
			// One answer of the GitHub Releases API, read for the beta channel. A
			// longer one is refused as not a release list.
			Name: ReleaseListBytes, Class: Buffer, Unit: Bytes,
			Limit: 1 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.maxReleaseListBytes"},
		},
		{
			// Entries in one release archive. More is refused as archive_unsafe and
			// nothing of it is kept.
			Name: ReleaseArchiveEntries, Class: Buffer, Unit: Rows,
			Limit: 20000, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.maxArchiveEntries"},
		},
		{
			// What one release archive unpacks to. More is refused as archive_unsafe
			// and nothing of it is kept.
			Name: ReleaseUnpackedBytes, Class: Buffer, Unit: Bytes,
			Limit: 2 << 30, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.maxUnpackedBytes"},
		},
		{
			// The new binary's `version --json` before anything switches to it. At
			// the limit the update is recorded failed with smoke_run_failed.
			Name: ReleaseSmokeTimeoutSeconds, Class: Buffer, Unit: Seconds,
			Limit: 15, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.SmokeTimeoutSecondsLimit"},
		},
		{
			// How long the supervisor waits for a restarted daemon to serve its
			// console and name its commit. At the limit the update is rolled back
			// with health_timeout.
			Name: ReleaseHealthWaitSeconds, Class: Buffer, Unit: Seconds,
			Limit: 60, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.HealthWaitSecondsLimit"},
		},
		{
			// How long an update may stay pending. A new release that starts after
			// it gives the update up (boot_guard) and exits for the previous release
			// to start.
			Name: ReleasePendingDeadlineSeconds, Class: Buffer, Unit: Seconds,
			Limit: 600, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.PendingDeadlineSecondsLimit"},
		},
		{
			// Starts of a new release while its update is pending. At the limit its
			// boot guard rolls the update back.
			Name: ReleaseBootAttempts, Class: Buffer, Unit: Rows,
			Limit: 3, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.BootAttemptsLimit"},
		},
		{
			// Starts of the update supervisor for one update. Past the limit it
			// stops trying the new release and rolls back (supervisor_gave_up).
			Name: ReleaseSupervisorRuns, Class: Buffer, Unit: Rows,
			Limit: 5, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.SupervisorRunsLimit"},
		},
		{
			// The age at which an update.lock left by a process that died is taken
			// over; a younger one refuses a second update with update_in_progress.
			Name: ReleaseLockStaleSeconds, Class: Idempotency, Unit: Seconds,
			Limit: 1800, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.LockStaleSecondsLimit"},
		},
		{
			// How often a daemon with a staged app bundle looks whether the app
			// has quit; a running app keeps its bundle until the next look.
			Name: ReleaseAppSwapPollSeconds, Class: Observation, Unit: Seconds,
			Limit: 2, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.AppSwapPollSecondsLimit"},
		},
		{
			// How long a daemon watching a running app trusts the pids it last
			// listed before it runs `ps` again; a pid reused by another process
			// delays the swap by at most this long.
			Name: ReleaseAppRelistSeconds, Class: Cache, Unit: Seconds,
			Limit: 60, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.AppRelistSecondsLimit"},
		},
		{
			// How often an auto-apply held back by a busy session looks again
			// whether the sessions are idle; it stops once the update starts or
			// nothing is left to apply.
			Name: ReleaseAutoApplyRetrySeconds, Class: Observation, Unit: Seconds,
			Limit: 300, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.AutoApplyRetrySecondsLimit"},
		},
		{
			// Store snapshots taken before an update (VACUUM INTO). The oldest is
			// removed when a new one is written.
			Name: ReleaseBackupsKept, Class: Journal, Unit: Rows,
			Limit: 2, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.BackupsKeptLimit"},
		},
		{
			// Unpacked releases kept besides `current` after a healthy update; older
			// ones are removed. A commit-named source deploy is never removed.
			Name: ReleasePreviousKept, Class: Journal, Unit: Rows,
			Limit: 2, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.PreviousReleasesKeptLimit"},
		},
		{
			// Versions that rolled back, which auto-apply does not try again. The
			// oldest is forgotten first.
			Name: ReleaseFailedVersions, Class: Journal, Unit: Rows,
			Limit: 16, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.FailedVersionsLimit"},
		},
		{
			// One of the updater's own files (status, pending, failed, lock). A
			// longer one is reported unreadable, never read as idle.
			Name: ReleaseStateFileBytes, Class: Buffer, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.maxStateFileBytes"},
		},
		{
			// How long `clawdline update --apply` follows an update before it says
			// the outcome could not be read and exits 3; the update itself goes on.
			Name: ReleaseApplyFollowSeconds, Class: Buffer, Unit: Seconds,
			Limit: 1500, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"cmd/clawdline.applyFollowSecondsLimit"},
		},
	}
}
