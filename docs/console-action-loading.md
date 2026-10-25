# Console action waiting inventory

Source review: 2026-10-25, base `11cfbfe32a879c5ab320f76681880548b2868502`. This page preserves the bounded, single-pass inventory of major person-triggered actions under `web/console/src` and records the implementation approved on 2026-10-25. Initial data reading remains in [the skeleton inventory](console-loading.md); none of its SK/EX/NO IDs are changed.

[Open the Taiwan Traditional Chinese screenshot report](reports/action-loading-audit.html). The root must deliver that document through the canonical Clawdline Cloud document locator; this relative repository link is not Cloud acceptance.

The person approved AL-01–AL-16, AL-25 and AL-27 on 2026-10-25. These 18 entries are now implemented; AL-01 was already deployed separately in `2d26bfb1b`. The other entries retain their original recommendations (5 recommended, 1 conditional, 3 retained). IDs and feedback labels remain unchanged.

[Open the approved implementation screenshot report](reports/action-loading-implemented.html). The [original screenshot inventory](reports/action-loading-audit.html) remains available for comparison.

## Approved implementation

| IDs | Implemented feedback |
| --- | --- |
| AL-01 | Existing Playbook start spinner, busy state and reduced motion retained. |
| AL-02–AL-05 | Playbook and reusable-text saves/deletes expose request progress; snippet writes remain serialized through refresh, with a visible list status for ordering and scope changes. |
| AL-06–AL-09 | The selected task/answer/Note action exposes progress; create/edit submits retain a saving indicator across preparation and write stages. |
| AL-10–AL-14 | Proposal decisions and Board create/edit/delete/complete controls expose progress on the pressed action. Existing operation labels remain. |
| AL-15–AL-16 | Project instruction saving has an indicator on Save; detach exposes progress on the corresponding Project. Existing source-read and sync phases remain. |
| AL-25 | Ordinary schedule create/save/delete expose progress; cross-machine move wording remains owned by its existing staged flow. |
| AL-27 | Project removal gains a spinner beside its existing removing label. |

The shared 14 px indicator uses the existing English and Taiwan Traditional Chinese catalogs. Busy controls expose `aria-busy`, keep request and delivery receipts separate and stop rotating under reduced motion. Icon-only actions retain accessible labels. No server request types or completion criteria are changed.

The 18 approved representative states were rendered from the actual components with labeled synthetic data at 430 px, and checked at 390 px and 1024 px for action-control overflow. All had visible indicators and no recorded browser exceptions; reduced-motion checks passed. A separate full-console fixture delayed a real Playbook save, refused it, retried successfully and then deleted the Playbook. It verified the busy control, prevented duplicate requests, removed the indicator on failure, retained the error and completed the successful retry. Screenshot state fixtures do not establish production latency or every grouped action variant.

## Original review table

| ID | Name | Stable feedback label | Trigger | Current waiting behavior | Recommendation and reason | Priority | Source |
| --- | --- | --- | --- | --- | --- | --- | --- |
| AL-01 | Playbook start | 常用流程啟動 | Press Start | Baseline screenshot: disabled, Starting… text only. Current hosted version has a ring. | Completed in `2d26bfb1b`: small activity ring, aria-busy and reduced-motion handling. | Done | `web/console/src/session/Snippets.tsx:259`; `web/console/src/session/Snippets.tsx:808` |
| AL-02 | Session Playbook save | Session 常用流程儲存 | Save a Session Playbook | Save disables without a changed label. | Recommended: Show saving on the pressed control until the write and refresh settle. | P1 | `web/console/src/session/Snippets.tsx:584`; `web/console/src/session/Snippets.tsx:989` |
| AL-03 | Session Playbook delete | Session 常用流程刪除 | Confirm Playbook deletion | Delete icon disables without a visible action state. | Recommended: Keep a deleting indicator visible through the refresh. | P2 | `web/console/src/session/Snippets.tsx:609`; `web/console/src/session/Snippets.tsx:756` |
| AL-04 | Snippet writes and reordering | 常用句寫入與排序 | Save/delete/re-scope/reorder reusable text | A shared ref gates writes without rendering a busy state. The screenshot shows the current editor. | Recommended: Expose the particular pending write; includes custom Clawdfather text using the same pattern. | P1 | `web/console/src/session/Snippets.tsx:417`; `web/console/src/session/Snippets.tsx:625`; `web/console/src/session/Snippets.tsx:944` |
| AL-05 | Playbook page save and delete | 常用流程頁儲存與刪除 | Save or delete on the Playbooks page | Controls disable; action labels do not change. | Recommended: Name the pending operation on its own control. | P1 | `web/console/src/pages/workflow.tsx:225`; `web/console/src/pages/workflow.tsx:246`; `web/console/src/pages/workflow.tsx:330` |
| AL-06 | Session task actions | Session 待辦送出與狀態變更 | Send/complete/reopen/delete a Session task | Controls disable; existing sent/read receipts describe delivery, not this request. | Recommended: Distinguish request progress from later delivery receipts. | P1 | `web/console/src/session/Todos.tsx:153`; `web/console/src/session/Todos.tsx:427`; `web/console/src/session/Todos.tsx:439` |
| AL-07 | Session task create and edit | Session 待辦新增與編輯 | Create or edit a Session task | Submit disables without changed text; creation can also prepare and attach images. | Recommended: Keep a saving state visible in the form. | P1 | `web/console/src/session/Todos.tsx:235`; `web/console/src/session/Todos.tsx:261`; `web/console/src/session/Todos.tsx:280` |
| AL-08 | Session inline question reply | Session 選項回覆 | Choose a suggested Session reply | Choices disable immediately; this card has no sending label. | Recommended: Expose the pending delivery here, even when the separate pending-send system also tracks it. | P1 | `web/console/src/session/AsyncQuestion.tsx:37`; `web/console/src/session/AsyncQuestion.tsx:63` |
| AL-09 | Note reply and resolve | 便條紙回覆與完成 | Reply to, resolve or reopen a Note | Reply group has aria-busy; buttons only disable visually. | Recommended: Show the current sending/resolving action without hiding the Note. | P1 | `web/console/src/session/Interventions.tsx:179`; `web/console/src/session/Interventions.tsx:208`; `web/console/src/session/Interventions.tsx:417`; `web/console/src/session/Interventions.tsx:433` |
| AL-10 | Board proposal decisions | 看板提案接受與拒絕 | Accept or reject a Board proposal | Both buttons disable without a changed label. | Recommended: Identify the particular pending proposal and operation. | P1 | `web/console/src/pages/work/WorkV2.tsx:682` |
| AL-11 | Board item edit | 看板工作卡編輯 | Save Board item edits | Saving text is already present; no activity ring. | Conditional: A ring is optional if delay warrants it; retain the existing text. | P2 | `web/console/src/pages/work/WorkV2.tsx:1402` |
| AL-12 | Board item delete | 看板工作卡刪除 | Confirm Board item deletion | Deleting text is already present; no activity ring. | Conditional: A ring is optional; avoid flashing for short operations. | P2 | `web/console/src/pages/work/WorkV2.tsx:1423` |
| AL-13 | Board item creation | 看板工作卡建立 | Create a Board item | Creating text is present; image preparation can lengthen the operation. | Conditional: Optional small activity indicator, especially for attachments. | P2 | `web/console/src/pages/work/WorkV2.tsx:1544`; `web/console/src/pages/work/WorkV2.tsx:1581` |
| AL-14 | Board item completion | 看板工作卡完成 | Confirm Board item completion | Marking text and aria-busy already exist. | Conditional: Optional visual consistency change, lower priority than silent actions. | P2 | `web/console/src/pages/work/CompleteWorkDialog.tsx:27` |
| AL-15 | Project instruction file save | 專案指引檔儲存 | Save a project instruction file | A separate status line says saving; Save retains its normal label. | Conditional: Keep the status; optionally associate a small ring with Save. | P2 | `web/console/src/pages/projects/ProjectFiles.tsx:138`; `web/console/src/pages/projects/ProjectFiles.tsx:216` |
| AL-16 | Project sync and detach | 專案同步與解除同步 | Read a source, synchronize or detach | Source reads and bulk sync change labels; detach only disables. | Conditional: Retain existing sync labels; target detach if adding an indicator. | P1 | `web/console/src/pages/projects/ProjectSync.tsx:120`; `web/console/src/pages/projects/ProjectSync.tsx:132`; `web/console/src/pages/projects/ProjectSync.tsx:161` |
| AL-17 | Default model and effort save | 預設模型與推理強度儲存 | Choose a default model or effort | Fields disable, block has aria-busy, status is empty until completion. | Recommended: Identify the pending save while retaining the previous confirmed value. | P2 | `web/console/src/pages/settings/DefaultModelsBlock.tsx:56`; `web/console/src/pages/settings/DefaultModelsBlock.tsx:97` |
| AL-18 | Work gate settings save | 工作關卡設定儲存 | Toggle planning or verification defaults | Switches disable; old mode remains visible; block has aria-busy. | Recommended: Add saving text without treating the old mode as an acknowledgement. | P2 | `web/console/src/pages/settings/GateSettingsBlock.tsx:64`; `web/console/src/pages/settings/GateSettingsBlock.tsx:99` |
| AL-19 | Workflow settings save | 工作流程設定儲存 | Toggle workflow/access settings | Switches disable; block has aria-busy; status appears after success. | Recommended: Keep the last confirmed setting and add a save-in-progress cue. | P2 | `web/console/src/pages/settings/WorkflowSettingsBlock.tsx:37`; `web/console/src/pages/settings/WorkflowSettingsBlock.tsx:76` |
| AL-20 | Notification preference save | 通知偏好設定儲存 | Toggle a notification preference | All preference checkboxes disable without a saving indicator. | Recommended: Show which setting is being saved. | P2 | `web/console/src/pages/notifications.tsx:157`; `web/console/src/pages/notifications.tsx:187` |
| AL-21 | Browser pairing action submission | 瀏覽器配對操作送出 | Create a browser invitation or submit a pairing code | Native-bridge fixture: request busy disables controls before confirmation phases begin. | Conditional: Only animate request submission; preserve instructions during human confirmation. | P2 | `web/console/src/pages/settings/BrowserPairingBlock.tsx:43`; `web/console/src/pages/settings/BrowserPairingBlock.tsx:58` |
| AL-22 | Board assignment | 看板工作指派 | Assign existing or new Session | Activity ring, operation text, aria-busy and a status line already exist. | Not recommended: Keep the existing presentation; do not add a second animation. | Retain | `web/console/src/pages/work/WorkV2.tsx:1008`; `web/console/src/pages/work/WorkV2.tsx:1044` |
| AL-23 | Secret Note handoff | 便條紙機密交付 | Submit a secret Note | Sending and received states are distinct. | Not recommended: Keep existing semantics; receipt completes the handoff, consumption is separate. | Retain | `web/console/src/session/Interventions.tsx:262`; `web/console/src/session/Interventions.tsx:459` |
| AL-24 | Board step state | 看板步驟狀態 | Read Board step status | Read-only done/todo list; no pending person-triggered request. | Not recommended: Do not animate unfinished steps as if they were network requests. | Retain | `web/console/src/pages/work/WorkSteps.tsx:6` |
| AL-25 | Schedule save and delete | 排程儲存與刪除 | Create/save/delete a schedule | General form/delete paths use aria-busy and disabled controls without operation-specific pending text. Cross-machine move has its own wording. | Recommended: Add a specific write indicator; retain move stages and their failures. | P2 | `web/console/src/pages/schedules.tsx:764`; `web/console/src/pages/schedules.tsx:1426`; `web/console/src/pages/schedules.tsx:1535` |
| AL-26 | Cross-machine control permission | 跨機器控制權限變更 | Change cross-machine control permission | All switches disable while old state remains; no applying message. | Recommended: Name the in-progress two-sided change. | P2 | `web/console/src/cloud/PeerAccessPage.tsx:125`; `web/console/src/cloud/PeerAccessPage.tsx:171` |
| AL-27 | Project removal | 專案移除 | Confirm project removal | Removing text is present; no activity ring. | Conditional: Optional consistency improvement; existing text already communicates progress. | P2 | `web/console/src/pages/projects.tsx:119`; `web/console/src/pages/projects.tsx:441` |

## Existing adequate indicators: compact supporting review

These source-reviewed families support the decisions above. Only the numbered entries have per-entry screenshot evidence.

| Surface | Source (relative to `web/console/src`) | Decision |
| --- | --- | --- |
| Session creation and restore | `session/StartProgress.tsx:20`; `session/Restore.tsx:156`; `session/Restore.tsx:314` | Existing stages and restore phase labels; retain them rather than replacing them with a generic spinner. |
| Composer send and pending delivery | `session/Composer.tsx:500`; `session/Composer.tsx:751`; `cloud/PendingRemoteTurn.tsx:1` | Sending label and delivery tracking already distinguish request and observation. A persistent send must not claim receipt early. |
| Board assignment and decision answer | AL-22; `pages/work/WorkV2.tsx:1008`; `pages/work/WorkV2.tsx:1044`; `pages/work/WorkV2.tsx:697` | Assignment already has a ring; decision answers have their own pending/confirmed feedback. Keep it. |
| Secret Note handoff | AL-23; `session/Interventions.tsx:262`; `session/Interventions.tsx:459` | Preserve sending/received semantics and keep secret content out of screenshots. |
| Schedule Run now and move | `pages/schedules.tsx:2256`; `pages/schedules/run-loading.css:1`; `pages/schedules.tsx:1723` | Run now has data-loading and an activity ring; move has operation-specific text. No duplicate indicator proposed. |
| Project instruction unification | `pages/projects/ProjectUnify.tsx:185`; `pages/projects/ProjectUnify.tsx:275` | Apply sets an applying notice, disables the control and exposes aria-busy. Retain the notice; a second animation is not necessary. |
| Worktree removal | `pages/projects.tsx:449`; `pages/projects.tsx:453` | Has waiting text, aria-busy and a small ring; retain. |
| Squad skills, role creation and pack import/export | `pages/Squad.tsx:806`; `pages/Squad.tsx:816`; `pages/Squad.tsx:823`; `pages/Squad.tsx:871`; `pages/squad/PersonaCreator.tsx:77` | These submits already have specific pending text. This pass did not render every nested role setting, restoration or icon upload branch. |
| Update and restart | `machine/UpdatePanel.tsx:117`; `machine/UpdatePanel.tsx:252`; `machine/UpdatePanel.tsx:277` | Keeps semantic progress/restarting status and aria-busy. Do not replace download/apply/restart stages with an indefinite generic ring. |
| iTerm integration consent | `session/ITermConsentDialog.tsx:24`; `session/ITermConsentDialog.tsx:41` | Saving has its own label. Human consent remains a separate phase. |
| Documents reading and refresh | `pages/documents.tsx:88`; `legacy/js/view/documents.js:112`; `legacy/js/view/documents.js:245`; `legacy/js/view/documents.js:269` | Reading/connecting/error states already exist; first-list reading is SK-13. Sharing/copying/fullscreen are not evidence of a daemon mutation. |

## Actions and states not recommended for added animation

| State | Reason |
| --- | --- |
| Initial data reads | Keep SK/EX IDs in `console-loading.md`; action requests do not receive skeletons. |
| Board steps | AL-24 is a read-only progress projection, not a button awaiting acknowledgement. |
| Human login/pairing/permission confirmation | Keep concrete next-step instructions and expiry/refusal states; a ring must not imply automatic completion. |
| Terminal connections and streams | Keep connection or stream semantics; an unfinished stream is not an unfinished action request. |
| Local navigation, copy and browser preferences | Immediate UI/local operations do not justify an indefinite network-style animation. |
| Already adequate action indicators | AL-22 and AL-23 are retained examples; supporting rows above are source-reviewed, not screenshot acceptance for every branch. |

## Evidence method and boundaries

Chrome 155.0.8059.85 rendered the actual source components through a task-owned Vite server bound to 127.0.0.1, at 430 × 780 CSS pixels and device scale 1. The fixture uses the actual maintained zh-Hant catalog and product styles. A temporary Vite transform exposes otherwise private components and wraps React state hooks with explicitly fixed synthetic states; it does not replace their rendered markup or create proposed UI. All browser fetch calls are held by the fixture and the native pairing bridge is a synthetic promise. No request reaches the running daemon on 7727, a Cloud account or a paid API. Captures show current controls, including current missing indicators, never a drawn proposal. The label at the top of every image identifies the AL ID, name and synthetic fixed-state provenance.

The first restricted Chrome CDP launch aborted with SIGABRT. Repeating the same isolated launch with the required browser permission produced a real screenshot. Initial fixture attempts exposed a missing Vite package subpath alias, missing parent styles, and entry effects that cleared seeded state; these were corrected in the task harness before the final captures. Final exception/readiness results and harness files are retained in task artifacts for the root. These fixture issues are not product defects.

The review covers Session/Playbook/Snippet actions, Board assignments/proposals/forms/steps, Notes, project file/sync/removal actions, settings, browser pairing/cross-machine control, updates, schedules and documents. Supporting source review also includes Squad and instruction unification. It does not establish real production latency, complete network error behavior, iOS/WebKit rendering, native permission acceptance, account login or production Cloud document rendering. Only the representative state named for each grouped action was captured: e.g. one create form covers create/edit, one permission switch covers enable/disable, and the Playbook delete icon is shown in its detail context. Secondary variants remain unverified; no claim is made that every nested action branch across the console has been exercised. Admin/billing actions, media editors, every Squad preference/restore/upload branch and archived Session lifecycle variants are outside this screenshot set.

Recommended additions should be small (around the existing 14 px activity-ring scale), local to the pressed action and accompanied by operation text, disabled/reentry protection and accessible busy/status semantics. Reduced motion must remove rotation while retaining a static cue and text. Preserve accepted/executed/delivered/observed/acknowledged distinctions; an animated request cannot prove the downstream result. Conditional items already have meaningful text and should follow evidence of delay rather than a blanket animation rule.

## Document limits and validation

The report is static HTML with inline JPEG screenshots only: no scripts, remote URLs, live content or proposed-interface mockups. The document reader caps the file at 2 MiB (`internal/adapters/documents/documents.go:41`), each decoded image at 8192 per side / 16 Mi pixels, and cumulative image pixels at 64 Mi (`internal/adapters/documents/html_images.go:15`). The report uses the sanitizer-supported static elements and styles (`internal/adapters/documents/html.go:18`). Final size, hashes, image count and capture readiness are recorded in the task verification artifact. Local browser acceptance does not establish Cloud acceptance.


## Verification outcome

The final 27 component captures all reached readiness with zero recorded browser exceptions. The current Go HTML/image sanitizer retained all 27 images (678,536 bytes after sanitization). The actual client `htmlPreviewSource` then applied DOMPurify and its CSP wrapper; Chrome decoded all 27 images at 390 px viewport width, with 390 px body width, 27 numbered articles, zero scripts and the expected Traditional Chinese heading. The HTML source is 678,638 bytes and contains 9,055,800 decoded image pixels, below the documented limits.

The HTML report conservatively selected the full local gate. The restricted child run passed 28 checks and failed Go suites plus the synthetic DMG harness. A root callback with native system access passed 29 checks, including the DMG harness, and failed only `TestConfigureDispatchShutdownCancelsLease`. The failure was an empty `config-writes` file: the fixture creates that file before writing the request, while the test shut down the peer as soon as the file existed. A temporary 25 ms gap between create and write reproduced the failure. Waiting for the exact request bytes instead passed three separate processes with the same injected gap. The temporary fixture delay was then removed; only the test's synchronization condition changes. No application behavior is changed by this test repair. The final scoped gate covers this test module, repository guards and the report.
The child did not commit, push, deploy, publish, install a dev build, or create a Session delivery receipt, as required by its briefing. Integration, Git commit and canonical Cloud delivery are performed by the root.
