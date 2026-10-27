# Console loading inventory

This inventory records the 2026-10-20 source review and gives each screen a stable feedback ID. Keep IDs and names when priority or implementation status changes; do not renumber the table. The Taiwan Traditional Chinese names are the person's reference labels.

SK-01 was implemented and browser-checked in the original inventory. The person subsequently approved the 19 Recommended entries (SK-02, SK-04, SK-09–SK-17, SK-19, SK-21–SK-24, SK-26, SK-30 and SK-31). These now use skeletons; Conditional entries remain proposals. EX entries already have skeletons; NO entries should retain meaningful action or connection state. The review covered console pages, Session panels, project dialogs, Cloud entry points, admin panels and onboarding statistics. It did not measure production waiting times or establish end-to-end coverage for every loading branch.

Person-triggered asynchronous writes and submissions have a separate [action waiting inventory](console-action-loading.md), with AL IDs and a screenshot report.

## Candidate screens

| ID | Name | Feedback label | Recommendation | Priority | Source |
| --- | --- | --- | --- | --- | --- |
| SK-01 | Usage overview | 使用概況 | Implemented | P1 | `web/console/src/pages/usage.tsx:10`; `web/console/src/pages/usage/UsageOverview.tsx:85` |
| SK-02 | All-machine Session list | 所有機器的 Session 清單 | Implemented | P1 | `web/console/src/cloud/FleetSessionList.tsx:428`; `web/console/src/cloud/FleetSessionList.tsx:436` |
| SK-03 | Cloud machine picker | Cloud 機器選擇清單 | Conditional | P2 | `web/console/src/cloud/CloudGate.tsx:2448` |
| SK-04 | Account machine list | 裝置頁的帳號機器清單 | Implemented | P2 | `web/console/src/pages/devices/AccountMachines.tsx:129` |
| SK-05 | Machine browser device list | 機器的瀏覽器裝置清單 | Conditional | P2 | `web/console/src/pages/devices/AccountMachines.tsx:514` |
| SK-06 | Cross-machine permissions | 跨機器權限 | Conditional | P2 | `web/console/src/cloud/PeerAccessPage.tsx:155` |
| SK-07 | Account plan | 方案內容 | Conditional | P2 | `web/console/src/legacy/js/view/plan.js:253`; `web/console/src/pages/plan.tsx:34` |
| SK-08 | Notification preferences | 通知偏好設定 | Conditional | P2 | `web/console/src/pages/notifications.tsx:184` |
| SK-09 | Verification list | 驗收清單 | Implemented | P1 | `web/console/src/pages/verify.tsx:173` |
| SK-10 | Verification detail | 驗收紀錄詳情 | Implemented | P1 | `web/console/src/pages/verify.tsx:266` |
| SK-11 | Scheduled task list | 排定工作清單 | Implemented | P1 | `web/console/src/pages/schedules.tsx:284`; `web/console/src/pages/schedules.tsx:339`; `web/console/src/pages/schedules.tsx:2657` |
| SK-12 | Scheduled task run history | 排定工作的執行紀錄 | Implemented | P2 | `web/console/src/pages/schedules.tsx:2120` |
| SK-13 | Session document list | Session 文件清單 | Implemented | P1 | `web/console/src/legacy/js/view/documents.js:185`; `web/console/src/legacy/js/view/documents.js:206`; `web/console/src/pages/documents.tsx:1` |
| SK-14 | Project setup and readiness | 專案設定與準備狀態 | Implemented | P1 | `web/console/src/pages/projects/ProjectSetup.tsx:251` |
| SK-15 | Project file explorer | 專案檔案瀏覽器 | Implemented | P1 | `web/console/src/pages/projects/ProjectExplorer.tsx:117`; `web/console/src/pages/projects/ProjectExplorer.tsx:128` |
| SK-16 | Project instruction files | 專案指引檔案 | Implemented | P2 | `web/console/src/pages/projects/ProjectFiles.tsx:181` |
| SK-17 | Project worktree list | 專案工作樹清單 | Implemented | P2 | `web/console/src/legacy/js/view/worktrees.js:329` |
| SK-18 | Add-project folder picker | 新增專案的資料夾選擇器 | Conditional | P3 | `web/console/src/pages/projects/ProjectFolderPicker.tsx:76` |
| SK-19 | Local and project terminal lists | 本機與專案終端清單 | Implemented | P1 | `web/console/src/session/TerminalList.tsx:107`; `web/console/src/pages/terminal/TerminalProjectList.tsx:127` |
| SK-20 | Work item gate detail | 工作卡的關卡詳情 | Conditional | P2 | `web/console/src/pages/work/WorkGate.tsx:177` |
| SK-21 | Work item token bill | 工作卡的 Token 帳單 | Implemented | P2 | `web/console/src/pages/work/TokenBill.tsx:97`; `web/console/src/pages/work/TokenBill.tsx:104` |
| SK-22 | Work assignment Session picker | 工作指派的 Session 選擇清單 | Implemented | P2 | `web/console/src/pages/work/WorkV2.tsx:1249` |
| SK-23 | Work-linked Session task detail | 工作關聯 Session 的待辦詳情 | Implemented | P2 | `web/console/src/pages/work/WorkV2.tsx:1285` |
| SK-24 | Session task panel | Session 待辦面板 | Implemented | P1 | `web/console/src/session/Todos.tsx:179` |
| SK-25 | Session intervention panel | Session 待處理事項面板 | Conditional | P2 | `web/console/src/session/Interventions.tsx:328` |
| SK-26 | Session Git panel | Session Git 面板 | Implemented | P2 | `web/console/src/legacy/git-bridge.ts:93`; `web/console/src/legacy/git-bridge.ts:119`; `web/console/src/session/GitPanel.tsx:31` |
| SK-27 | Background command output panel | 背景命令輸出面板 | Conditional | P2 | `web/console/src/session/ShellPanel.tsx:160` |
| SK-28 | My messages list | 我的訊息清單 | Conditional | P3 | `web/console/src/session/UserMessages.tsx:151`; `web/console/src/session/UserMessages.tsx:227` |
| SK-29 | Resource coordination panel | 資源協調面板 | Conditional | P3 | `web/console/src/session/ResourceCoordination.tsx:70` |
| SK-30 | Playbook skill detail and source | 常用流程的技能詳情與來源 | Implemented | P2 | `web/console/src/pages/playbook.tsx:342`; `web/console/src/pages/playbook.tsx:361` |
| SK-31 | Squad skill import sources | 角色小隊的匯入技能來源 | Implemented | P2 | `web/console/src/pages/Squad.tsx:853`; `web/console/src/pages/Squad.tsx:859` |
| SK-32 | Settings capacity monitoring | 設定的容量監測區塊 | Conditional | P3 | `web/console/src/pages/settings/CapacityBlock.tsx:103` |
| SK-33 | Admin campaign list | 管理後台的活動清單 | Conditional | P3 | `web/console/src/cloud/admin/PromoManagement.tsx:269` |
| SK-34 | Admin campaign detail and redemption codes | 管理後台的活動詳情與兌換碼 | Conditional | P3 | `web/console/src/cloud/admin/PromoManagement.tsx:577`; `web/console/src/cloud/admin/PromoManagement.tsx:781` |
| SK-35 | Onboarding survey statistics | 新手問卷統計 | Conditional | P3 | `web/console/src/onboarding/Statistics.tsx:64` |

## Existing skeletons and states to retain

| ID | Name | Feedback label | Decision |
| --- | --- | --- | --- |
| EX-01 | Single-machine Session list | 單機 Session 清單 | Keep the existing skeleton |
| EX-02 | Session transcript | Session 對話 | Keep the existing skeleton |
| EX-03 | Cloud Session initial detail | Cloud Session 詳情初開 | Keep the existing skeleton |
| EX-04 | Session console screen | Session 主控台畫面 | Keep the existing skeleton |
| EX-05 | Session snippets and Playbook lists | Session 快捷內容與常用流程清單 | Keep the existing skeleton |
| EX-06 | Session start history list | 開啟 Session 的歷史對話清單 | Keep the existing skeleton |
| EX-07 | Project list | 專案清單 | Keep the existing skeleton |
| EX-08 | Squad main page | 角色小隊主頁 | Keep the existing skeleton |
| EX-09 | Playbook main list | 常用流程主清單 | Keep the existing skeleton |
| EX-10 | Archive list | 封存清單 | Keep the existing skeleton |
| EX-11 | Work board | 看板 | Keep the existing skeleton |
| EX-12 | Machine dashboard | 機器儀表板 | Keep the existing skeleton |
| EX-13 | Cloud terminal | Cloud 終端 | Keep the existing skeleton |
| EX-14 | Cloud all-machine terminal list | Cloud 所有機器終端清單 | Keep the existing skeleton |
| EX-15 | Admin overview | 管理後台總覽 | Keep the existing skeleton |
| NO-01 | Send and write actions | 傳送與寫入動作 | Keep meaningful state and progress |
| NO-02 | Session startup and update progress | 啟動與更新進度 | Keep meaningful state and progress |
| NO-03 | Authentication pairing and permission checks | 登入、配對與權限確認 | Keep meaningful state and progress |
| NO-04 | Terminal connections and live streams | 終端連線與持續串流 | Keep meaningful state and progress |
| NO-05 | Settings controls and project sync operations | 設定控制項與專案同步操作 | Keep meaningful state and progress |
| NO-06 | Attachment image loading | 附件圖片載入 | Keep meaningful state and progress |

## First-read behavior

Usage overview shares a static skeleton between the lazy page and the first data read. Its totals, chart areas and distribution sections use neutral placeholders without sample numbers or invented trends. Narrow screens stack the distribution sections. A reduced-motion media rule removes pulsing. A machine change starts its own initial read; project and period filters, refreshes, errors and retries retain the existing loading presentation.

For future candidates, retain existing rows during refresh when they are still valid. Keep accessible loading text with decorative blocks hidden from assistive technology. Never replace real send, save, delete, dispatch, update, authentication, pairing or streaming progress with a skeleton. Low-frequency panels and fixed controls need a case-by-case decision; a source loading branch alone does not prove a visible delay worth redesigning.

## Recommended screens implementation

The 19 approved screens share decorative row, card, detail and text geometry through `LoadingSkeleton.tsx` and `loading-skeleton.js`. Each loading region carries its stable `data-loading-id`. The shapes use existing neutral theme tokens, hide decorative content from assistive technology, keep the existing translated loading label, delay their appearance to avoid flashing on quick reads, and stop the sheen for reduced motion. Narrow panels shrink without horizontal overflow.

Fleet Sessions keep each machine heading and its offline or refusal explanation. Device cards, verification rows and details, schedules and their history, document lists, project readiness, explorer directories and file contents, instruction files, worktrees, terminal lists, token bills, assignment choices and their task details, Session tasks, Git files and diffs, Playbook sources and squad import sources now have an initial-reading footprint. File lists and instruction content use independent regions; loaded directory context stays visible while a selected file is read.

Existing retained rows remain authoritative during refresh where the screen already supports retention. Send, save, delete, assignment, terminal creation, retry errors, permission checks and other meaningful progress retain their current presentation. Skeletons contain no invented numbers, diff signs, active controls or real data. No read transport, retry policy, cache or request budget changes.
