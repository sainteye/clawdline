# macOS permission audit, 2026-10-28

This audit covers the daemon, macOS shell, installer, updater, browser permission
entry points in the current Go/React product. The correction covers the App's own unnecessary
reads and the timing of its permission-capable operations. Agents and other tools
are outside this correction; their access is not an App permission defect. It does
not audit or modify the archived Swift application. A system dialog naming
Clawdline is evidence of OS attribution, not sufficient evidence of which
executable performed the access or of which file was read.

## Confirmed defects and corrections

| Entry point | Before | Correction |
|---|---|---|
| `projects.scratchMarks` | Resolving every exclusion root traversed `Documents/Codex` and the retired application's `Library/Application Support` directory. The separate durable-place guard did not cover this call. | Keep lexical exclusions; resolve only roots that pass the background policy. |
| Claude and Codex project history | Only literal `~/Library` paths were excluded. A historical cwd in Desktop, Documents, Downloads, Pictures, Music or Movies could reach directory checks and repository/setup readers. | Filter all seven personal roots before label, canonicalization, stat and downstream metadata work. |
| Live working directories in the start-place list | Visibility of an assistant process implicitly admitted its cwd as a Project. | The same background policy applies. Saved protected Projects remain visible from persisted records; their directory is checked when selected for use. |
| Dev-stack discovery | Icon-registry paths and process cwds bypassed the start-place exclusion entirely. | Gate automatic discovery before the first `.devstack.json` lookup, for both sources. A selected Session's info/Links request still inspects that one Project. |
| Update ancestry discovery | Only `~/Library` was excluded before spawning Git. | Exclude all personal roots and unchosen symlink paths from this optional lookup. |
| Session list project keys | Optional `project_key` enrichment walked Git metadata from any observed cwd. | Omit this optional field for paths outside the background policy. The Session remains visible. |
| Machine-workspace classification | Resolving a foreign cwd could touch personal data just to decide whether it was a daemon workspace. | Reject unsafe foreign paths before resolution. Preserve a direct alias to the owned workspace by reading the link itself and independently checking its parent and target. |

On macOS, automatic candidates containing symbolic links are conservatively
excluded without following those links. The OS aliases `/var`, `/tmp` and `/etc`
are spelled under `/private` before checking components. A missing safe component
is allowed to reach the existing missing-file handling. Arbitrary aliases require
explicit Project registration; registration already canonicalizes the selected
path. Lexical checks do not mistake `MusicNotes` for `Music`. On macOS they also
exclude mounted volumes under `/Volumes` and compare personal-root spellings
without case sensitivity.

These gates avoid unintended probes; they are not a race-proof filesystem sandbox.
Other platforms retain the existing symlink behavior. Personal-root exclusions
extend the existing cross-platform Library exclusion to the other six roots.
Registered personal Projects remain usable. Adding or re-adding a Project is an
explicit directory selection: it validates the directory and saves its Project
key at that time. Listing those records later does not revalidate protected
locations, query their repository or scan their setup. Optional update ancestry
and Session-list project-key enrichment avoid those locations as well.

A selected Start still validates the chosen cwd before launching; a selected
Project's files, tree, memory and worktree routes resolve that Project on use.
Worktree identity resolution skips unrelated protected entries. A saved Project
key preserves the worktree entry without re-reading Git during list rendering.
Legacy saved rows without that optional key remain launchable; explicitly adding
the same Project again refreshes the key without changing its registration time.
An explicit project-sync request remains allowed to inspect the offered Projects.

Permission-capable operations that are necessary for the feature the person
just requested are retained: recording, pairing-camera use in a browser,
attachment selection, enabling notifications, enabling iTerm2 integration,
Project registration/start/detail, model setup and document opening. No new
permission-denial response or blanket prohibition was added to these actions.

## Other permission-capable surfaces inspected

| Surface and source | Trigger / observation | Audit outcome |
|---|---|---|
| Installer and updater: `install.sh`, `cmd/clawdline/setup.go`, `adapters/install`, `adapters/release/updater` | Installation/state directories, LaunchAgent plist, release payloads; purge enumerates only its configured state directory. | No default recursive home search found in these paths. Custom installation/state paths and their links can still name protected locations. |
| Native menu: `shell/darwin/MenuReadings.swift` | Launch/menu refresh checks CLI versions and Whisper model directories. | Default model discovery already excludes the retired application's Application Support. User model directories and executable resolution still follow links. |
| Whisper daemon: `adapters/whisper/whisper.go` | Capability/status reads discover models, even without recording. | Default roots are `.cache/whisper` and `models` on macOS; an explicit model or symlink into protected data remains capable of a prompt. |
| iTerm2: `adapters/terminal/iterm*.go`, `launch_darwin.go`, `stall_darwin.go` | Apple Events for scanning, capture, sending, revealing and launching. | Existing `iterm_integration` consent gate defaults off, including legacy installations. Existing system Automation grants do not enable it. This change does not alter that gate. |
| Dictation: `legacy/voice-bridge.ts`, `shell/darwin/Microphone.swift`, `Browser.swift` | The recording gesture calls audio-only `getUserMedia`; native delegates accept the intended origin's microphone request. | Intentionally permission-capable after recording is requested. Native delegates do not independently prove a user gesture; the calling page owns that boundary. |
| Camera: `cloud/pair-camera.ts`, `PairPanel.tsx` | Explicit pairing QR camera flow in a browser. | Intentionally permission-capable; native shell delegates reject camera capture. |
| Image attachments: native `runOpenPanelWith` delegates | Attachment file input opens an image-only system picker. | Explicit file selection; no direct PhotoKit library authorization entry point found in the inspected shell. The picker can expose personal locations selected by the person. |
| Notifications: `push/push.ts`, `support/notifications.tsx` | Enable handlers call `Notification.requestPermission`; visit/resume reads existing registration. | Permission request is in the enable flow. Subscription resumption is distinct from initial consent. |
| Clipboard: terminal paste handlers, copy buttons, `adapters/artifacts/pasteboard_darwin.go` | Explicit paste/copy; native picture delivery uses the pasteboard. | Permission-capable browser/native clipboard surfaces remain tied to their actions. The native pasteboard helper is not an iTerm2 Apple Event. |
| Global shortcut: `shell/darwin/HotKey.swift` | `RegisterEventHotKey`, rather than a global key monitor. | No accessibility-request or input-monitoring-request API found in this implementation. |
| WebKit persistence and signing | Native WebKit stores browser state; app replacement can change its code hash. | Previous measured keychain re-prompts are not disproved by a stable designated requirement. Packaging preserves an unchanged shell, but shell changes still require separate native acceptance. |
| Process discovery: `adapters/process/openfiles_darwin.go` | `lsof -a` restricts descriptor/cwd observations to requested assistant PIDs. | Does not recursively enumerate the home directory. An observed cwd is not filesystem consent. |
| Project/task documents: `adapters/documents/documents.go` | A document request discovers an `artifacts` root and resolves paths; project-root symlink following is configurable, task roots are contained. | Document list/open is an explicit content request. Preserve its existing containment and selected-root behavior; do not suppress a necessary OS prompt when the person opens a document. |
| Git and setup metadata: `projects/repo.go`, `identity.go`, workspace verification, setup readers | Explicit registered Projects, worktree `.git` indirection and `commondir`, setup inspection. | Automatic list enrichment is gated. Explicit registration, Project detail and project-sync operations remain permission-capable; these reads belong to the selected feature. |
| Provider records and icon registry | Known `.claude`, `.codex`, `.gemini` account locations; some readers follow configured/legacy symlinks. | These are bounded provider sources, not personal-library APIs. Relocating them through a symlink can still reach a protected location. No complete no-follow policy was added to every provider adapter. |
| Agents and their tools: process/session launch adapters | Agents may run shell searches, browser automation or computer-use helpers. | OS attribution can name the daemon, but agent/tool behavior is outside this correction. No agent restrictions or daemon-level sandbox were introduced. |

## Evidence and limits

The original scratch-root and personal-history regression cases failed on the
pre-correction behavior: both protected scratch roots reached the resolver, and
six previously unfiltered personal-root categories reached the label callback.
Tests use synthetic homes, not the person's photos, media or application data.
Further cases check zero filesystem callbacks for lexical personal roots, aliases
that never reach their target, explicit registration, both dev-stack sources,
optional Session keys and update probes. Follow-up regressions also caught
eager validation of saved protected Projects and the initial over-broad suppression
of servers in a deliberately selected Project. Both cases were corrected.
The paired cases verify that saved list rows cause no repository/setup work,
while selected repository/worktree reads and Session starts still succeed; a
deleted selected cwd is refused at actual use.

The cross-platform alias regression exposed an OS-specific path assumption:
the helper rewrote Linux `/tmp` as macOS `/private/tmp`, then accepted the
nonexistent component before reaching the alias. The `/private` normalization
is now limited to Darwin; Linux keeps its real path components. The alias
regression remains enabled on both platforms.

A read-only local TCC log examination covered a six-hour window. It showed
Clawdline-attributed requests involving both the native shell and descendant
programs, including computer-use helpers. Attribution/request/preflight records
are not proof that a dialog was displayed, permission was granted or data was
read. The provided dialog could not be joined to an exact requesting syscall and
protected path from this window; it is not assigned a definitive root cause.
Raw private OS logs are excluded from the repository.

No TCC permissions were reset, granted, suppressed or bypassed. No personal
library was enumerated, and no running daemon or app was replaced for this audit.
A fresh-user macOS installation/reinstallation test, with pristine TCC state and
separately exercised opt-in features, is still required before claiming that an
installed release no longer produces unexpected prompts. Source tests do not
provide that receipt. In particular, a pre-existing project `artifacts` symlink may itself name another
application's Application Support directory. Inspecting the link is not the same as
opening its target; this audit does not write a delivery report through that link.
The inspected table distinguishes automatic work from expected feature access;
it is not a request to suppress every system dialog. A clean-user reinstall run
is still a separate acceptance step. The intended result is fewer unsolicited
App requests, with necessary permission requests preserved at deliberate use.
