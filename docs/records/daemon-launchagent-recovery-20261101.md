# LaunchAgent update and recovery checks

## Observed failure

A v2.12.0 installation updating to a local development build stopped its
LaunchAgent to migrate the legacy CLI path to `service/clawdline-daemon`.
The first `launchctl bootstrap` failed with exit 5 (I/O error). Both releases
carried the named daemon, so rollback left the plist unchanged and attempted
`kickstart` on an unregistered job. That failed with exit 113 (service not
found). The updater nevertheless recorded `rolled_back` and retired its
supervisor. Nothing listened on the daemon port.

The precise OS cause of bootstrap's I/O error was not established. Its failure
and the absent-job rollback are sufficient to reproduce the recovery defect.
Re-registering the existing stable LaunchAgent restored v2.12.0 and console HTTP
200 without replacing the release or restarting terminal sessions.

## Automated regression checks

Three new tests failed against the original implementation: retrying a transient
bootstrap error, restoring an unloaded job when both releases use the same
executable, and retaining recovery when the previous service has not answered.
The fixed updater passes those checks, its complete package suite, and tests
for caller cancellation, exhausted recovery, and a late candidate health answer
racing with a boot-guard rollback. The boot guard retains the rollback phase and
original typed reason until the supervisor verifies the previous service.

The service-command timeout and supervisor-run budget are unchanged. Failure to
recover after that budget records `failed`; it does not assert that service was
restored. Persistent permission, signing, missing-file or OS failures still need
operator intervention.

## Native launchd drill

Run from the exact candidate checkout in a macOS GUI login:

```sh
CLAWDLINE_TEST_LAUNCHD_RECOVERY=1 go test -count=1 -v \
  ./internal/adapters/release/updater -run '^TestLaunchdUpdateRecovery$'
```

The opt-in test creates a unique temporary LaunchAgent, installation, state and
ephemeral loopback port. It uses executable HTTP fixtures and real launchctl
commands. The first candidate bootstrap is deliberately failed after real
bootout. Both a legacy previous release and a previous release already carrying
the named daemon must reach candidate console HTTP 200 and the expected BUILD.
The job is then deliberately unloaded and rollback must re-register it, reach
previous console HTTP 200 and match its BUILD. Cleanup unloads only the test's
own job before its temporary directory is removed. Both cases passed on
2026-11-01; this is fixture acceptance, not signed-release acceptance.

## Required release acceptance

A normal Go test run and CI always execute the portable failure-injection suite.
The native drill is opt-in because a macOS GUI launchd domain is required.
Before claiming a public release fixes upgrades, additionally test the actual
signed archives in an isolated installation:

1. Start with the actual previously published CLI/updater. Test both a loaded
   legacy CLI LaunchAgent and a loaded named-daemon LaunchAgent. Test a fresh
   installation separately. The helper fixture does not prove these binaries.
2. Apply the candidate through the previous release's normal updater. Verify
   the running version, console HTTP 200, matching BUILD version and commit,
   existing Session IDs, Cloud reconnect and unchanged terminal processes.
3. Repeat with candidate startup/health failure and with bootstrap failure.
   Verify the previous version and BUILD return, including when both releases
   use the named daemon. A down port with `rolled_back` is a failed drill.
4. Interrupt and resume the supervisor during migration and rollback. Verify
   pending recovery survives, retries remain bounded, and failure exhaustion is
   reported accurately. Retain the original typed errors and evidence.
5. Perform a second successful update from the fixed version and repeat the
   failure drill; fixes to an updater first govern updates *from* that version.

An already-installed v2.12.0 updater remains its published bytes; shipping a new
binary does not retroactively repair its supervisor. Do not treat the fixture
drill as proof that this first transition is protected. If that transition fails
acceptance, block publication until a verified migration/recovery path is in
place, or require an explicit repair of the existing service before updating.
Do not replace signed historical assets or silently modify an installed release.

For an existing outage, inspect the selected release and managed plist, preserve
the updater's status, and re-register that exact service in its recorded launchd
domain. Verify the actual console and BUILD, rather than only health or the
launchctl exit code. This recovery must be performed locally because an offline
daemon cannot receive a Cloud command. Never start a second writer against the
live state directory or restart terminal servers to repair the daemon.
