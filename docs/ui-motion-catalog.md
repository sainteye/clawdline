# UI motion catalog

For maintainers and reviewers selecting motion changes, this register gives each interaction a stable identifier so feedback keeps the same subject across releases. It records proposals, not authorization to implement them.

## Identity and evidence

The source audit is pinned to `85d42421ec8e0968290b15515b1d9c11cf60927e` (2026-10-20). Two read-only audits covered Session interactions and global pages. The root checked the navigation drawer's final stylesheet order: `main.tsx` imports `sidebar-scroll.css` after `legacy/pages.css`; historical comments in the latter do not describe the current cascade.

Line numbers in the source column refer to that commit and have since moved. The implemented first-batch elements carry a matching `data-motion-id` attribute (for example `data-motion-id="AN-001"`), which locates them in current source.

Do not renumber or reuse IDs. The next unused ID is AN-032. Keep an old localized name as an alias when renaming; use child IDs when splitting a scope. The Taiwan Traditional Chinese names below are the exact feedback labels issued to the person.

AN-001 through AN-023 were cataloged as proposals or improvements. The first approved batch is recorded below; the remaining proposals still require selection. AN-024 through AN-031 describe existing behavior and are retained for feedback. Screenshots in the companion review artifact use production React components and CSS with synthetic data; they locate targets and do not prove animation quality, authenticated Cloud flows, or full acceptance.

## First approved batch

AN-001, AN-002, AN-003, AN-004, AN-009 and AN-014 are implemented for review. The shared transitions are in `ui/disclosure-motion.css`, imported by `main.tsx` after `sidebar-scroll.css`; the to-do disclosure's height animation is in `ui/use-details-motion.ts`. Production deployment requires separate approval; the earlier HTML atlas remains a pre-implementation target map.

The Session action menu, composer suggestions and Project picker retain only a non-interactive closing picture through discrete display transitions. Hidden content is inert immediately. Unsupported engines keep immediate controls. More commands keeps its existing level slide and observes reduced motion.

The native to-do disclosure measures its body only on a toggle and animates height and padding for 220 ms. Its summary's requested expanded state and the body's inert state change immediately; native content remains open only until the closing picture finishes. Rapid reversal cancels the old animation after reading its current height. Data refreshes do not restart it. Attention Notes use a reversible grid transition; secret fields still unmount immediately on close, nested details retain their prior reset behavior, and secret completion does not acquire a second exit transition. Composer selection state still clears immediately; only the last displayed suggestion rows survive for the exit picture.

Chromium and WebKit passed at 390px and 1280px with synthetic data: open/close, intermediate heights, reversal, keyboard focus, nested controls, secret-field clearing, suggestion filtering and reduced motion. Disabling the new stylesheet made menu opening immediate (opacity 1); enabling it produced intermediate opacity. In one recorded run at 390px the to-do disclosure closed from 258.56px through 106.53px to 38.39px in Chromium, and from 257px through 107.58px to 38px in WebKit. These runs verify the production components locally and do not claim authenticated Cloud acceptance.

## Interactions

| ID | Interaction / feedback label | Recommendation | Proposed scope / retained behavior | Source below `web/console/src` |
|---|---|---|---|---|
| AN-001 | Session action menu / Session 動作選單 | Recommended first | Enter from the trigger with a short fade and reverse on exit. | `session/Detail.tsx:748; legacy/detail.css:146` |
| AN-002 | More commands level / 更多指令換頁 | Recommended first | Keep the existing level slide; align timing and reduced-motion behavior. | `session/Detail.tsx:526; legacy/detail.css:159` |
| AN-003 | Session to-do disclosure / Session 待辦展收 | Recommended first | Complete the existing entrance with a reversible height transition on close. | `session/Todos.tsx:157; pages/work/work.css:453` |
| AN-004 | Attention Notes panel / 關注便條展收 | Recommended first | Add reversible panel disclosure; keep secret-handoff success independent. | `session/Interventions.tsx:308; session/interventions.css:1` |
| AN-005 | Note detail disclosure / 便條細節展收 | Recommended later | Reveal recent outcomes and long Note detail without abrupt displacement. | `session/Interventions.tsx:331; session/Interventions.tsx:375` |
| AN-006 | Background work drawer / 背景工作抽屜 | Recommended later | Complete the existing entrance with an exit. | `session/BackgroundStrip.tsx:61; session/background.css:29` |
| AN-007 | Tool action disclosure / 工具動作群組展收 | Recommended later | Animate manual tool disclosure while preserving the reading position. | `session/Transcript.tsx:1050` |
| AN-008 | Code change disclosure / 程式變更卡片展收 | Recommended later | Reveal a diff without measuring large content on every frame. | `session/Transcript.tsx:1767` |
| AN-009 | Composer suggestions / 輸入建議選單 | Recommended first | Animate opening and closing once; do not replay while filtering suggestions. | `session/Composer.tsx:585; legacy/composer.css:59` |
| AN-010 | Attachment row / 附件列增減 | Recommended later | Smooth attachment row height changes without scaling the composer. | `session/Composer.tsx:563; legacy/composer.css:91` |
| AN-011 | Voice input row / 語音輸入列切換 | Recommended later | Transition the control row without inventing recording state during permission checks. | `session/Composer.tsx:578; legacy/composer.css:155` |
| AN-012 | Forms and confirmation dialogs / 表單與確認視窗 | Recommended later | Align to-do forms, work forms and confirmation dialogs; retain focus and submission semantics. | `session/Todos.tsx:227; pages/work/WorkV2.tsx:1378; pages/work/work.css:666; legacy/sheets.css:5` |
| AN-013 | Session view switch / Session 檢視切換 | Defer for now | Defer pane transitions until scroll, focus and live-view lifecycle are verified. | `session/Detail.tsx:142; legacy/detail.css:205` |
| AN-014 | Project picker / 專案選單展收 | Recommended first | Match the machine picker timing while preserving each entry point layout. | `ui/ProjectPicker.tsx:69; ui/project-picker.css:18` |
| AN-015 | Work detail dialog / 工作詳情開關 | Recommended later | Fade the backdrop and move the detail slightly from its card context. | `pages/work/WorkV2.tsx:1336; pages/work/work.css:666` |
| AN-016 | Board section disclosure / 看板區塊展收 | Recommended later | Reveal completed items, proposals, sources and long descriptions without competing with card FLIP. | `pages/work/WorkV2.tsx:449; pages/work/WorkV2.tsx:1100; pages/work/work.css:108` |
| AN-017 | Device detail sheet / 裝置詳情抽屜 | Recommended later | Use a short bottom entrance on phones and a small entrance on desktop. | `pages/devices/AccountMachines.tsx:207; pages/devices/account-machines.css:60` |
| AN-018 | Device detail tabs / 裝置詳情分頁 | Recommended later | Transition the selected indicator and incoming tab content. | `pages/devices/AccountMachines.tsx:229; pages/devices/account-machines.css:86` |
| AN-019 | Project folder disclosure / 專案資料夾展收 | Recommended later | Animate only the subtree operated by the person. | `pages/projects/ProjectExplorer.tsx:38; pages/projects/ProjectFiles.tsx:43` |
| AN-020 | Schedule disclosure / 排程區塊展收 | Recommended later | Reveal the schedule list and explanation sections without losing scroll position. | `pages/schedules.tsx:1089; pages/schedules.tsx:1930; legacy/schedules.css:50` |
| AN-021 | Settings tabs / 設定分頁切換 | Recommended later | Fade the selected pane without sliding the whole settings screen. | `pages/settings.tsx:265; pages/settings/page.css:43` |
| AN-022 | Squad detail navigation / 小隊詳情切換 | Recommended later | Clarify role-to-detail navigation and skill disclosure without stacking use feedback. | `pages/Squad.tsx:334; pages/Squad.tsx:783; pages/squad/squad.css:227` |
| AN-023 | Usage result transition / 用量結果銜接 | Defer for now | Defer chart animation; prioritize stable result-region geometry and readable data. | `pages/usage/UsageOverview.tsx:81; pages/usage/UsageOverview.tsx:132` |
| AN-024 | Navigation drawer / 左側導覽抽屜 | Retain existing | Retain the existing discrete-display transition and immediate fallback. | `main.tsx:43; sidebar-scroll.css:12` |
| AN-025 | Brand handle and chevrons / 品牌按鈕與箭頭 | Retain existing | Retain the existing handle and chevron transition. | `brand-handle.css:22` |
| AN-026 | Machine picker / 機器選單展收 | Retain existing | Retain the existing picker transition delivered by 849d94785. | `cloud/cloud.css:115` |
| AN-027 | Fleet machine group / 機器 Session 群組展收 | Retain existing | Retain the existing group transition delivered by 849d94785. | `cloud/cloud.css:523` |
| AN-028 | Board card movement / 看板卡片移位 | Retain existing | Retain the existing card FLIP and cancellation. | `pages/work/board-motion.ts:17` |
| AN-029 | Jump to latest button / 回到最新訊息按鈕 | Retain existing | Retain the existing entrance, exit and hit-test handling. | `session/jump.css:17` |
| AN-030 | Settings switches / 設定開關切換 | Retain existing | Retain the existing thumb and background transitions. | `pages/settings/switch.css:16` |
| AN-031 | Squad use feedback / 小隊使用回饋 | Retain existing | Retain the existing setting-controlled use feedback. | `pages/squad/squad.css:146` |

## Acceptance before implementation

Animate explicit person actions, not every token, poll, retained snapshot or status update. Closing immediately removes hit testing and keyboard access; focus returns to the appropriate trigger. Rapid toggles reverse cleanly. Reduced motion switches immediately. Sending, interrupting and ending never wait for decoration. Do not scale text or move the whole transcript. Validate phone WebKit and desktop Chromium, keyboard navigation, long content and updates during reversal.

The first proposed batch is AN-001, AN-002, AN-003, AN-004, AN-009 and AN-014. Tool output, diffs, trees and variable-length content need scroll and layout-cost checks. AN-013 and AN-023 are deferred because the reading and lifecycle costs currently exceed the expected benefit; this is a recommendation, not removal from scope.

Do not add token-by-token entrances, terminal-output animations, rolling metric numbers, continuous bounce, or another transform on elements already controlled by card movement or use feedback.
