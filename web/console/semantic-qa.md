# Console catalog semantic review

Format validation and semantic review are separate gates. Run `node web/console/tools/semantic-risk-keys.mjs --check` and `node web/console/tools/group-semantic-keys.mjs --check` from the repository root to reproduce the sets. The release candidate has **3,389 top-level keys**, including `lang` and `dir`, and **3,387 content keys**. Risk categories overlap: negation 767, state and delivery 516, permission and pairing 187, deletion and undo 71, and uncertainty 102. Their deduplicated union is **1,340 keys**. The uncertainty category added 75 unique keys after the original 1,265-key sweep. `semantic-risk-keys.json` lists every key and category.

Reviewers compare English, Taiwan Chinese, and the target entry. They check actor, scope, negation, permission, irreversible effects, whether an operation was attempted, accepted, delivered, acknowledged, completed, or refused, and whether unavailable evidence means unknown rather than absent. Each U+001F branch is checked in context. A catalog passing keys, placeholders, plural branches and HTML validation is still a draft until semantic review is reported.

| Language | Reviewed and corrected | Remaining limit |
| --- | --- | --- |
| en | Feature Root read all 1,340 risk source entries and authored the 13 complete count templates. | Other 2,047 entries are source text; rendered context needs E2E. |
| zh-Hant | Feature Root compared all 1,340 risk entries to English and authored all count templates. Corrected acknowledgement, payment acceptance, applicability, skill creation, unknown deployment/image state and generic No. | 2,047 non-risk entries were not individually re-reviewed for this gate. |
| zh-Hans | Feature Root compared all 1,340 risk entries to English and Taiwan Chinese and authored all count templates. Same state corrections, plus deployment terms and a model-name typo. | 2,047 non-risk entries were not individually re-reviewed for this gate. |
| ja | Catalog owner reviewed the original 1,265 risk keys. Feature Root reviewed 75 more, authored the count templates, and audited **587 distinct non-risk keys** by screen group. Root candidate has 125 revisions against the owner's draft, including three wrongly joined JSX fragments replaced by full templates. | **1,460 non-risk keys lack individual review**. Whole-catalog natural Japanese is not certified. |
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

The Epic machine-word guard, expanded to detect Korean `맥`, reports `machine words: 9 files clean` when run on these nine catalog values. An additional ASCII-boundary scan finds zero `Mac`/`Macs` values even where CJK text touches the word without spaces, and a Korean scan finds only the three contextual exceptions above. The localhost Chrome fixture at 390px and 1280px rendered 378 paragraphs, including `legacy.webFailNotFound`, `legacy.webShowOnMac` and `settings.webCloudPairOrderScan` in all nine languages; it found no forbidden machine name, blank text or horizontal overflow. Catalog validation again reports nine complete catalogs, 3,389 top-level keys each, no missing keys or format errors. This is a terminology review of the affected values; it does not certify the remaining Japanese or Korean natural-language entries.

## Japanese screen-group audit

`semantic-screen-groups.json` divides the **2,047 non-risk keys into 69 flow groups** using the source inventory. The Feature Root read every English, Taiwan Chinese and Japanese entry in 12 groups (**466 current non-risk keys**), plus all 10 non-risk full count templates. The 56 other groups supplied two deterministic sampled keys each except single-key `Console / Sessions`: **111 more distinct keys**, for **587 inspected** and **1,460 without individual review**. The four additional Board modal labels were authored and checked separately in all nine languages after a real browser flow exposed them. Groups can be cross-referenced to source files in the generated JSON.

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

Sampling is reproducible: sort keys in each group by SHA-256 of `postfix:<key>` and take the first two. After correcting groups with findings, sort by SHA-256 of `after-flow:<key>` and take the first five for a second comparison. Additional 12-key resamples of Projects/files and Work/Board found two more style/context problems in the former and no new severe reversal in the latter. Reviewer: Feature Root. This is a text review and cannot certify the other 1,456 Japanese entries or visual context.

## Future partial catalogs and screen readers

After baseline v1, a secondary catalog may omit a newly added key. The page keeps its actual selected `lang`, counts missing keys in `data-i18n-missing`, and fills each missing value from English. The language picker marks an English fallback label or hint with `lang="en"`. Existing legacy controls and many direct text expressions cannot mark only the fallback phrase without changing their DOM structure. A screen reader may pronounce those future English fallback phrases using the page language; this remains a UX review item if secondary gaps become common.
