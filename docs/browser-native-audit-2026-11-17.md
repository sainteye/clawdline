<!-- clawdline-doc: kind=record audience=both -->
# Browser features inside the macOS App

Source baseline: `51927b3b713b8b2ded6ff8c3ed9d0e7c8631eaae`, inspected on
2026-11-17. This audit covers the current console and `shell/darwin`, including
the separate local `Shell` and hosted `CloudWeb` delegates. It excludes the
retired application, experiments and third-party code. A source finding does
not prove which version or screen another person's machine was running.

## Confirmation decisions

There were ten browser-native confirmation calls in seven production files:

| File under `web/console/src` | Decisions |
| --- | --- |
| `pages/schedules.tsx` | Run now; explicit force retry after `schedule_active` |
| `pages/playbook.tsx` | Delete a Playbook |
| `pages/Squad.tsx` | Change scope with handbook drafts; restore an inherited handbook |
| `pages/projects/ProjectFiles.tsx` | Change files; discard an edited file |
| `pages/projects/ProjectSetup.tsx` | Leave project settings with unsaved files |
| `support/HelpCenter.tsx` | Leave an unsaved help/community draft |
| `cloud/admin/PromoManagement.tsx` | Leave a one-time promotion code |

The local WebView uses `Shell` as its UI delegate (`main.swift`, `buildWindow`).
Its `WKUIDelegate` extension in `Microphone.swift` has no native confirmation
method. The installed Apple SDK's `WKUIDelegate.h` explicitly states that an
unimplemented confirmation method behaves as Cancel. `CloudWeb` implements
that method in `Browser.swift`, but it is a separate delegate. Run now returned
before setting its loading state or sending its request, explaining a completely
inert press on the local App screen independently of the selected model.

All ten calls now use `confirmAction` in `web/console/src/confirm-action.ts`.
It uses an application-owned HTML dialog and returns a promise. Nothing is
approved by opening it, pressing Enter on its initial container, closing it,
Escape, owner cancellation or page unloading. Concurrent requests are declined,
not queued. Text is inserted as text, the keyboard stays in the dialog, and
focus returns to the opener. Labels use the existing English and Taiwan
Traditional Chinese catalogs. The module can also receive action-specific
labels and an AbortSignal.

The schedule's force retry awaits its separate decision; an unresolved promise
is never treated as approval. Project navigation retains its synchronous veto
and retries the requested destination after approval through the same guards.
Admin navigation also awaits the decision and rejects a stale owner's answer.
Browser `beforeunload` is a separate lifecycle and is not replaced by an async
dialog during unload.

## Other gaps found in source

These findings are recorded for follow-up; this change does not grant new
device permissions or alter the native file picker.

| Feature | Evidence and affected behavior | Classification |
| --- | --- | --- |
| Non-image file selection | Both `Microphone.swift` and `Browser.swift` set `NSOpenPanel.allowedContentTypes = [.image]`. The current pages also request ZIP imports (`pages/Squad.tsx`, `view-plugins/Host.tsx`), JSON reports (`doctor/DoctorBlock.tsx`), and arbitrary Bot attachments (`project-assistants/Composer.tsx`). Those files cannot be selected through these App pickers. | Confirmed native/UI contract mismatch. Read each input's constraints and preserve origin and main-frame checks when fixing. |
| Camera capture | Both media delegates grant only `.microphone` and deny camera requests. `cloud/PairScanner.tsx` and `cloud/pair-camera.js` offer live camera scanning. The native package configuration also needs review before requesting a camera. Choosing a QR image or entering the code remains a separate route. | Confirmed denial in source; not a hardware or OS permission diagnosis. |
| A script-owned preview window | `pages/work/useReferenceImage.ts` opens a blank window, retains its handle, and later assigns the fetched full image URL, potentially a Blob URL. Both native `createWebViewWith` implementations return nil; Cloud refuses non-HTTP(S) new-window URLs, and local links outside the console are handed to `NSWorkspace`. A page cannot steer a returned child WebView because none is created. | Confirmed window-control mismatch; the exact external-browser symptom has not been reproduced in this audit. Use an in-app preview or the supported download path. |
| Leaving the App while a one-time code is visible | `cloud/admin/PromoManagement.tsx` registers browser `beforeunload`. The shell has no corresponding code/draft close guard, and `applicationShouldTerminate` immediately returns `.terminateNow`. Closing the main window normally preserves its loaded WebView, whereas quitting destroys it. | No App quit protection in source; whether a particular WebKit reload/navigation shows a warning remains unverified. An async in-page confirmation cannot protect process termination by itself. |

## Features checked without a demonstrated missing bridge

| Feature | Existing handling and limits |
| --- | --- |
| `alert`, `prompt`, `print` | No production browser calls found. Local `Shell` has no alert/prompt handler; Cloud has an alert handler and no prompt handler. The native-dialog guard now rejects new explicit browser alert/prompt/confirm calls. |
| Ordinary links and `target="_blank"` | Both delegates handle new-window links: local console URLs load locally; Cloud URLs load in its view; other eligible URLs open in the person's browser. This does not provide a script-controlled popup handle. |
| Downloads and save dialogs | Both delegates route download navigation into `WebDownloads`, which uses `NSSavePanel` and a staging file. Current call sites include image, JSON, and Bot output downloads. This source audit does not establish success for every MIME type and URL scheme. |
| `showSaveFilePicker` | `session/lightbox-steps.ts` checks availability and falls back to an anchor download; absence is not itself a broken save path. |
| Microphone, recording and audio | Both delegates explicitly allow the microphone on the trusted origin. Voice code checks recording support and has legacy AudioContext fallbacks. Actual recording and playback were not exercised by this audit. |
| Image file input | Both delegates have a file-panel implementation and honor multiple selection. The same image restriction is appropriate here and inappropriate for ZIP/JSON/arbitrary attachments. |
| Folder selection | `session/Start.tsx` has a dedicated native folder-selection bridge (`FolderChooser.swift`); it does not depend on the image-only open panel. |
| Web Push and service workers | `push/push.ts` and `support/notifications.tsx` check Notification, PushManager and service-worker availability. The shell has no Web Push notification bridge. Availability depends on the actual WebKit runtime and App configuration; unsupported detection must not be reported as successful subscription. |
| Clipboard read/write | Many actions use the Clipboard API. Terminal paste has an explicit failure/native-entry fallback (`pages/terminal/keys.ts`); several copy controls expose failure or leave selectable text. Some only ignore failure. No custom clipboard permission delegate is present, but that alone does not prove engine-provided clipboard support is absent. Actual App gesture and permission behavior remains to be tested. |
| Web Share | Image sharing checks API/file support and has a download path; output-link sharing has a copy fallback. A missing OS share sheet is not automatically a broken action. |
| Fullscreen | `pages/documents/fullscreen.ts` always supplies an in-page fullscreen layer and makes the browser Fullscreen API optional. The native shell does not explicitly enable element fullscreen. The existing CSS layer remains useful without it. |
| QR decoding | The scanner supplies its own decoder; it does not require a native BarcodeDetector implementation. Camera permission remains the separate gap above. |
| Persistent storage and Web Locks | IndexedDB/local storage are used for installed views and browser state. Web Locks call sites check availability. No missing native delegate was established; storage lifetime, eviction and cross-tab coordination still require runtime-specific testing. |
| Location, device APIs and native browser file-system pickers | No production geolocation, notification-constructor, pointer-lock, Bluetooth/USB/serial/HID, credential-manager, open-file-picker or directory-picker call was found in this audit. The guarded save picker is listed separately above. |

## Verification and acceptance boundary

The structural guard was run against the original seven files: it failed and
listed all ten calls. The changed source passes that guard. Behavioral tests
cover deferred force decisions and released/replaced navigation owners. Browser
tests exercise the shared module in Chromium and WebKit, and the real console's
Run now, project-file and Playbook flows against synthetic local HTTP fixtures.
They do not start vendor CLIs, dispatch real tasks, contact paid APIs, grant
camera access or establish production deployment or native App installation.
Exact results and remaining failures belong in the delivery receipt.
