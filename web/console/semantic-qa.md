# Console catalog semantic review

Format validation and semantic review are separate gates. Run `node web/console/tools/semantic-risk-keys.mjs --check` and `node web/console/tools/group-semantic-keys.mjs --check` from the repository root to reproduce the sets. The release candidate has **3,389 top-level keys**, including `lang` and `dir`, and **3,387 content keys**. Risk categories overlap: negation 767, state and delivery 516, permission and pairing 187, deletion and undo 71, and uncertainty 102. Their deduplicated union is **1,340 keys**. The uncertainty category added 75 unique keys after the original 1,265-key sweep. `semantic-risk-keys.json` lists every key and category.

Reviewers compare English, Taiwan Chinese, and the target entry. They check actor, scope, negation, permission, irreversible effects, whether an operation was attempted, accepted, delivered, acknowledged, completed, or refused, and whether unavailable evidence means unknown rather than absent. Each U+001F branch is checked in context. A catalog passing keys, placeholders, plural branches and HTML validation is still a draft until semantic review is reported.

| Language | Reviewed and corrected | Remaining limit |
| --- | --- | --- |
| en | Feature Root read all 1,340 risk source entries and authored the 13 complete count templates. | Other 2,047 entries are source text; rendered context needs E2E. |
| zh-Hant | Feature Root compared all 1,340 risk entries to English and authored all count templates. Corrected acknowledgement, payment acceptance, applicability, skill creation, unknown deployment/image state and generic No. | 2,047 non-risk entries were not individually re-reviewed for this gate. |
| zh-Hans | Feature Root compared all 1,340 risk entries to English and Taiwan Chinese and authored all count templates. Same state corrections, plus deployment terms and a model-name typo. | 2,047 non-risk entries were not individually re-reviewed for this gate. |
| ja | Catalog owner reviewed the original 1,265 risk keys. Feature Root reviewed 75 more, authored the count templates, and audited **1,681 distinct non-risk keys** by screen group. Root candidate has further revisions against the owner's draft, including three wrongly joined JSX fragments replaced by full templates. | **366 non-risk keys lack individual review**. Whole-catalog natural Japanese is not certified. |
| ko | Catalog owner reviewed the original 1,265 risk keys. Feature Root checked 75 more, authored count templates and revised 20 uncertainty or state entries in the candidate. | 2,047 non-risk entries have not all received individual review; owner sampled machine-translation errors and did not certify whole-catalog Korean. |
| es | Catalog owner reviewed the original 1,265 risk keys. Feature Root checked 75 more, authored count templates and revised 12 uncertainty or state entries. | 2,047 non-risk entries have not all received individual review. |
| pt-BR | Catalog owner reviewed all 1,340 risk keys, including the extra 75, and translated the complete count templates. It corrected notification delivery, dead-letter uncertainty, pairing-key meaning, terminal keystroke replay and Brazilian terminology. | 2,047 non-risk entries have not all received individual review. |
| fr | Catalog owner reviewed all 1,340, including the extra 75; Feature Root authored count templates and corrected `legacy.webImageUnknown`. | 2,047 non-risk entries have not all received individual review. |
| de | Catalog owner reviewed all 1,340, including the extra 75; Feature Root authored count templates. Owner corrected permission, pairing, sign-in and update-command semantics. | 2,047 non-risk entries have not all received individual review. |

The 25 fixed sample keys, five per category, sit inside the full union. They are a cross-check rather than a substitute for the union:

| Category | Sample keys |
| --- | --- |
| Permission | `inline.7bdb8640f903`, `legacy.webFailReadOnly`, `next.cloudTerminalDefaultRead`, `next.cloudTerminalDefaultHelp`, `literal.3e7bd562c1a0` |
| Deletion and undo | `legacy.webScheduleDeleteAsk`, `verify.removeAsk`, `verify.removeAskOpen`, `literal.74df92b721ee`, `next.cloudDeviceRemoveAsk` |
| Refusal and uncertain delivery | `legacy.webFailForbidden`, `legacy.webFailSignedOut`, `next.cloudMachinesRefusedSignIn`, `template.f02affbcd988`, `template.a11c575f16d3` |
| Sign-in and pairing | `legacy.webConnLocked`, `legacy.webDoorFinished`, `legacy.webDoorToPair`, `next.cloudAccessProblem`, `settings.webCloudPairOrderWhy` |
| Deployment | `legacy.webInfoNoDeploy`, `literal.3056db4ed234`, `next.linksDeployUnreadable`, `template.292c86e4825b`, `template.65519076782c` |

## Traceable corrections

| Key | Before → after meaning |
| --- | --- |
| `legacy.webInfoCloseReasonCompletion` | Chinese “not received” → completion notice **not acknowledged**. |
| `legacy.webPlanWaiting` | Chinese payment merely sent → payment **accepted**, while Clawdline confirmation remains pending. |
| `next.proposalResolve` | Chinese “no longer exists” → “no longer applies.” |
| `next.pushCloudUnreached` | Chinese notification not received → machine **did not accept** the notification. |
| `literal.4cd467a2eb33`, `literal.d356ce36d4f9` | Chinese/Japanese catalog created → skill added to the catalog, while persona addition remains unconfirmed. |
| `next.linksDeployUnreadable`, `next.linksGitUnreadable`, `legacy.webImageUnknown` | Unreadable evidence remains **unknown** rather than asserting absence. |
| `work.answerNo` | Chinese generic “No” → 否, rather than “no need.” |
| `literal.725c63536f66` | Japanese/Korean/Spanish save success → **save outcome unconfirmed**, reloading. |
| `literal.aff43bfb749b` | Korean/Spanish capacity available → **capacity unavailable**. |
| `next.machineSessionUnknown` | Korean/Spanish now preserve both machine-inventory uncertainty and Session-online uncertainty. |
| `next.cloudMachinesRefusedSignIn`, `next.cloudDeviceRemoveAsk` | Spanish/French/German corrections preserve re-sign-in, immediate access loss and device-slot scope. |
| `verify.removeAskOpen` | Spanish/French corrections preserve unverified deletion and irreversible effect. |
| `next.sendUnsubmitted` | German now says Enter was **not pressed**. |
| `literal.7ddacedc1cff`, `literal.7331776e6ff4`, `next.terminalReacquired` | Brazilian Portuguese now preserves whether a notification was sent, whether dead-letter state is unknown, and that earlier terminal keystrokes are **not replayed**. |
| `legacy.webStartOff`, `literal.629ac7ab4009` | Japanese now preserves disabled Session start or remote writes and the actual enablement/local command path. |
| `next.transcriptJumpLatest` | Japanese “latest article” → latest message. |
| `count.skillFoldersSkipped`, `count.unfinishedWork` | Two wrongly joined source fragments became complete sentences with ordered placeholders. The old `inline.90ce0f66f094`, `inline.7851595d6a1c`, `inline.43da62a44d17`, `inline.8b35a4648619` and `inline.f6f098a567d5` keys were removed. |

The new pinned Projects/worktree keys selected by the risk generator are `projects.statusBoardUnavailable`, `projects.statusUpdateFailed`, `worktree.cleanupUnavailable`, `worktree.failed`, `worktree.inventoryNotObserved`, `worktree.lastObserved`, `worktree.noBranch`, `worktree.noWorktrees`, `worktree.notObserved`, `worktree.originUnknown`, `worktree.ownerUnknown`, `worktree.purposeUnknown`, `worktree.refreshed`, and `worktree.statusUnreadable`. Root checked English and both Chinese sources; target owners included them in the original 1,265-key sweep.

## Cross-platform machine terminology

The Feature Root compared all **97 English entries** that called a generic host a “Mac” with their screen context. None described Apple hardware or macOS (the separate `next.cloudMachinePlatformMac` value remains `macOS`). The same affected values were checked and changed in all nine catalogs: en 97, zh-Hant 97, ja 97, zh-Hans 94, ko 62, es 97, pt-BR 96, fr 0, de 97; **737 value edits** total. Fewer entries in some target catalogs already used a neutral term. Korean now distinguishes the host device from the browser device. German case endings, Spanish articles and the billing row were corrected after replacement. The source keys retain their stable names, including historical `*Mac*` keys, so the pinned legacy adapter still resolves them.

The first guard pass exposed a gap: 33 Korean values still called the generic host `맥`. The Feature Root compared and rewrote all 33 against English and Taiwan Chinese, including pairing (`legacy.webDoorAskFailed`), refusal (`legacy.webFailNotFound`), action (`legacy.webShowOnMac`) and Cloud delegation (`next.cloudPairAgentHand`). The three remaining occurrences mean macOS, context and textual context, respectively: `literal.ed6c1de76c4f`, `next.infoContext`, `work.usageBaseUnknown`. Chinese spacing was corrected in 94 Taiwan and 91 Simplified values, and Japanese spacing in 66 values after the noun change. The QR-scan instruction was rewritten as a complete localized button label in both Chinese variants, Spanish, Portuguese and German. German `legacy.webCloudStatusReading` was corrected from a doubled genitive ending.

A second source comparison of the Korean host sentences corrected 13 contextual errors. Most critically, `legacy.webNotifyOffUntold`, `legacy.webNotifyOn` and `legacy.webNotifyTestNone` again distinguish the browser device from the host; `legacy.webCoordWhyMachineTokenOnly` states that the **paired browser** lacks the host token; `legacy.webFailVoiceHostAmbiguous` points to the Devices list. The other corrections cover the verification ledger, Project access, disabled schedule dispatch, terminal selection, stale page and missing Whisper model. These are direct source-to-target revisions rather than a whole-catalog fluency certification.

The same host-related source comparison corrected 6 Spanish, 5 Brazilian Portuguese, 7 German and 1 Japanese values. `legacy.webStartOff` now means remote Session start **is disabled** rather than about to be switched off; `legacy.webNotifyOn` names the browser device as the subscriber; `legacy.webShowOnMacAsked` in Japanese, Portuguese and German now reports that a request **was sent** rather than an unexplained transfer or command; `legacy.webVoiceSlow` refers to the first transcription after restart. Spanish `legacy.webCloudStatusKey` no longer mixes English `Key` into its label, and German `legacy.webFailVoiceHostAmbiguous` names the Devices list and a masculine Rechner.

The Epic machine-word guard, expanded to detect Korean `맥`, reports `machine words: 9 files clean` when run on these nine catalog values. An additional ASCII-boundary scan finds zero `Mac`/`Macs` values even where CJK text touches the word without spaces, and a Korean scan finds only the three contextual exceptions above. The localhost Chrome fixture at 390px and 1280px rendered 378 paragraphs, including `legacy.webFailNotFound`, `legacy.webShowOnMac` and `settings.webCloudPairOrderScan` in all nine languages; it found no forbidden machine name, blank text or horizontal overflow. Catalog validation again reports nine complete catalogs, 3,389 top-level keys each, no missing keys or format errors. This is a terminology review of the affected values; it does not certify the remaining Japanese or Korean natural-language entries.

## Japanese screen-group audit

`semantic-screen-groups.json` divides the **2,047 non-risk keys into 69 flow groups** using the source inventory. The Feature Root read every English and Japanese entry in 38 groups (**1,612 current non-risk keys**), consulting Taiwan Chinese on ambiguous meanings, plus all 10 non-risk full count templates. The 30 other groups supplied two deterministic sampled keys each except single-key `Console / Sessions`: **59 more distinct keys**, for **1,681 inspected** and **366 without individual review**. The four additional Board modal labels were authored and checked separately in all nine languages after a real browser flow exposed them. Groups can be cross-referenced to source files in the generated JSON.

| Fully read group | Current non-risk keys | Error pattern and correction |
| --- | ---: | --- |
| Projects / files | 27 | File reload, skipped unreadable folders, and count-fragment grammar. |
| Work / Board | 146 | Pending versus completed, item creation, and action labels. |
| Projects / unify | 57 | Link direction, move-versus-link action, and exact local command. |
| Projects / other controls | 31 | Source/target icon flow and interrupted connection. |
| legacy / Coord | 50 | “Quiet watch” became a clock; coordination text was malformed. |
| legacy / Schedule | 34 | Dispatch disabled versus schedule valid, and failure notification. |
| legacy / Plan | 28 | Billing plan became a project plan; confirmed payment tense. |
| legacy / Start | 10 | Session start disabled became enabled. |
| legacy / Fail | 8 | Retry states and reconnecting tense. |
| legacy / Command | 12 | Voice command and draft action labels. |
| next / schedule | 16 | Invalid schema versus disabled schedule; offline last-seen time. |
| next / other | 47 | Text-size hint, unknown status, and latest message. |
| count / other | 10 | Complete number templates authored and checked for placeholder order in nine languages. |
| legacy / Door | 12 | Code lifetime, naming a device rather than calling it, digit position and restart action. |
| legacy / Cloud | 13 | Previous connection close, encryption repair and browser/machine observations. |
| legacy / Notice | 8 | Handoff receipt versus manual delivery, and workspace overlap. |
| Machine / dashboard | 28 | Overload versus cargo, busy versus running, available versus compatible memory, and headroom. |
| legacy / Settings | 8 | The assistant icon appears before Claude/Codex names in conversations. |
| legacy / Snippet | 18 | The source is the person's own last message, and moving a snippet changes its project scope. |
| legacy / Voice | 8 | Dictation was mistranslated as splitting a message; no detected audio was phrased as a person's failure to hear. |
| next / cloud | 68 | Machine renaming was presented as asking for the person's name; connecting was phrased as future action, and browser pairing button labels were literal or awkward. |
| next / terminal | 97 | “Nobody” wrongly asked who held control, F6 navigation was unintelligible, and a required user reconnect was phrased as the app's own action. |
| Settings / controls | 84 | Full was mistranslated as “hometown,” On as an unrelated phrase, pushed as “pressed,” and diagnostics copy as a past-tense sentence. |
| next / archive | 14 | Sidebar destination is a noun while the Archive button is an action; a reopened Session is not a file restore. |
| next / image | 10 | The annotation canvas, drawing gesture and picture tools had unrelated literal translations. |
| next / send | 9 | Enter was “press input,” a recorded failure was treated as a file, and checking delivery was described as search. |
| Work / other controls | 93 | “Confirm resolution” was read as image resolution, online/offline were read as actions to own a Session, and cancel/reassign status labels were misleading. |
| Console / Squad | 201 | “Disabled” became “disabled person,” persona collapse became “collapse/destroy,” and a safe-limit warning reversed how to let new Sessions start. |
| settings / other | 97 | Empty compact-window value meant “no intervention,” child autonomy was described as no confirmation, and QR code refresh looked like a future action instead of in-progress work. |
| work / other | 63 | “Your message” lost its actor, work whose delivery needed closing became mere confirmation, and the project directory source became a generic list. |
| verify / other | 41 | “Due in” became an error, start time became navigation to a beginning, and verification criteria became an unrelated count; metric headers were also transliterated or mislabeled. |
| legacy / Info | 51 | Uncommitted changes became “unwritten,” an agent still working became merely functioning, and Fast mode On became an unrelated phrase. |
| Projects / setup | 42 | A loading state became an instruction, `origin` became “origin point,” and “in progress” became a generic progress heading. |
| Projects / sync | 37 | The multiline explanation dropped the untracked file list, “up to date” became “latest article,” and an active sync became a generic noun. |
| worktree / other | 36 | No semantic reversal found; the status, residue, observation and target labels preserve the English states. |
| Session / Interventions | 28 | Sending a reply lost its pending state, a handled note had an unintelligible status, and reopening a reminder became a “grand opening.” |
| Session / other controls | 28 | A Project import/export prompt reversed who should ask for the Project, and paused verification was phrased as a completed action. |
| Session / Todos | 28 | Offline ownership reversed the actor, and image loading and sending became future commands rather than pending states. |
| legacy / Project | 24 | “Nothing to land” became a reference to land, and Git branch ancestry evidence was unintelligible. |

Sampling is reproducible: sort keys in each group by SHA-256 of `postfix:<key>` and take the first two. After correcting groups with findings, sort by SHA-256 of `after-flow:<key>` and take the first five for a second comparison. Additional 12-key resamples of Projects/files and Work/Board found two more style/context problems in the former and no new severe reversal in the latter. Four newly completed groups contained 61 keys and 18 corrected Japanese values; the previous two-per-group sample had already counted eight of those keys, so unique coverage rose by 53. Their five-per-group resample covered 20 keys and found two more style/context problems: `legacy.webCloudStatusReading` incorrectly read as an instruction and `legacy.webNoticeTimedOut` as an awkward noun label. Both were corrected and rechecked, for **20 revisions** in these four groups. Three additional completed groups contained 34 keys, added 28 unique reviewed entries, and led to five corrections; their 15-key resample found no further reversal. The 68-key Cloud group added 66 unique reviews and 10 corrections; the 97-key terminal group added 95 unique reviews and 11 corrections. Their five-key resamples found no further reversal. The 84-key Settings controls group added 82 unique reviews, made 19 initial corrections, and its five-key resample caught `literal.4abc45706b7c` missing the sending-progress tense; after correction it has **20 revisions**. The Archive, image markup and sending groups added 27 unique reviews and 13 corrections; their 15-key resample found no further reversal. The 93-key Work controls group added 91 unique reviews, made 19 initial corrections, and its five-key resample corrected `literal.5418f86ba3ea` from a transliterated “checker pass” label, for **20 revisions**. The 201-key Squad group added 199 unique reviews, made 21 initial corrections, and its five-key resample corrected `literal.743c4d79a088` from a declarative to an instruction, for **22 revisions**. The 97-key Settings group added 95 distinct reviews and 9 corrections, including `settings.settingsCompactWindowInvalid`, `settings.settingsOrchestratorPermission`, `settings.settingsSettle`, and `settings.webCloudPairRenewing`; its five-key resample found no additional semantic reversal. The 63-key Work group added 61 distinct reviews and 4 corrections (`work.answerTrack`, `work.createdViaQuote`, `work.decideLede`, `work.scopeSource`); its five-key resample found no additional reversal. The 41-key Verify group added 39 distinct reviews and 10 corrections, including `verify.criteria`, `verify.dueIn`, and `verify.startedAt`; its five-key resample found no additional reversal. The 51-key legacy Info group added 49 distinct reviews and 8 corrections, including `legacy.webInfoCloseReasonDirtyWorktree`, `legacy.webInfoCloseReasonWorking`, and `legacy.webInfoFastOn`; its five-key resample found no additional reversal. The 42-key Project setup group added 40 distinct reviews and 14 corrections, including `literal.77fe4c2aa7e2`, `literal.9391584df834`, and `literal.8d7dedc0c9d6`; its five-key resample found no additional reversal. The 37-key Project sync group added 35 distinct reviews and 14 corrections, including the assembled paragraph spanning `inline.efd366b4e300` through `inline.7633dc8d1def`, `literal.ebdff2f8a427`, and `literal.d2dca5f96f16`; its five-key resample corrected `literal.3c07629e48a7` spacing. The 36-key worktree group added 34 distinct reviews and no corrections; the five-key resample found no reversal. The 28-key Session intervention group added 26 distinct reviews and 7 corrections, including `literal.2c09858c5eed`, `literal.75420f3866c1`, and `literal.e0e3789d893d`; its five-key resample found no additional reversal. The 28-key Session controls group added 26 distinct reviews and 5 corrections, including `literal.673f223fa434` and `literal.1ebe900add97`; its five-key resample found no additional reversal. The 28-key Session to-do group added 26 distinct reviews and 7 corrections, including `literal.63c6995a9c0c`, `literal.7d2bc8bc67a4`, and `literal.84b95f498939`; its five-key resample found no additional reversal. The 24-key legacy Project group added 22 distinct reviews and 7 corrections, including `legacy.webProjectNothingToLand`, `legacy.webProjectNeedsNothingToLand`, and `legacy.webProjectEvidenceBranchBaseUnknown`; its five-key resample found no additional reversal. Reviewer: Feature Root. This is a text review and cannot certify the other 366 Japanese entries or visual context.

## Future partial catalogs and screen readers

The localhost Chrome Settings flow loaded all nine catalogs from the built bundle and checked the visible interface-language label against each catalog, the label's own `lang`, document `lang`/`dir`, and zero missing keys after reload. It also kept the separate Agent language setting and confirmed a broken Japanese response falls back atomically to English while preserving the saved browser preference. The focused test passed 1/1. This is a local fixture, not a Cloud deployment check.

After baseline v1, a secondary catalog may omit a newly added key. The page keeps its actual selected `lang`, counts missing keys in `data-i18n-missing`, and fills each missing value from English. The language picker marks an English fallback label or hint with `lang="en"`. Existing legacy controls and many direct text expressions cannot mark only the fallback phrase without changing their DOM structure. A screen reader may pronounce those future English fallback phrases using the page language; this remains a UX review item if secondary gaps become common.
