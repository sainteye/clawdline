# 第八波：圖片、語音、選單（2026-09-17 規劃，9/18 04:00 額度重置後平行開工）

使用者 2026-09-17 問：圖片上傳與拖拉、session 右上角選單、語音輸入是不是都還沒做，並指出這些本來可以平行。
三項確實都沒做（見下表），而且彼此獨立。使用者接著發現「在新版瀏覽器裡沒辦法選擇回答問題」，這是第四項（F）。當時 Claude 7 天額度 96%，使用者選擇「重置後一起開」。

| 項目 | 新版現況（master `813daf5`） | 舊版 |
|---|---|---|
| 附圖、貼上圖片、拖拉 | `#attach` 停用；貼上只收文字；沒有拖拉 | `input/shots.js` 212 行、`view/transcript-images.js` 842 行、`/v1/artifacts/images` |
| 語音輸入 | `#mic` 停用、`#voice` 隱藏 | `input/voice.js` 887 行 → `POST /v1/voice` → `Whisper.swift`（`whisper-cli`＋`ggml-*.bin`） |
| 在 Mac 上顯示、即時畫面 | `#tx-focus`、`#session-focus`、`#session-screen` 停用 | `/v1/sessions/<id>/focus`、`/screen`，`#screen-panel` |
| Git 變更、commit、push | 子選單開得出來，三項停用 | `input/git-panel.js` 127 行、`/v1/sessions/<id>/git`，commit／push 走確認框 |
| 文件、我傳出的訊息 | `#session-documents`、`#session-user-messages` 停用 | `view/documents.js` 318 行、`document-render.js`、`/documents` 系列路由；`input/user-messages.js` |
| 在瀏覽器回答問題（AskUserQuestion 等選單） | `#waiting` 一直隱藏；session 列沒有 `menu`；沒有 `/key` | `view/composer.js` 的等待卡；session 列的 `menu`（`RemoteServer.swift` 約 5353 行 `menuObject`，由畫面解析）；`POST /v1/sessions/<id>/key` 送選項的數字鍵 |

可用的：Session 資訊、關閉 session。常用句在舊版讀不到時本來就是隱藏，不在這一波。

## 怎麼平行

六個 child，**每個都在自己的隔離 worktree**（`isolation: "worktree"`）。原因：A 與 B 都要改
`session/Composer.tsx`，C、D、E 都要改 `session/Detail.tsx`，大家都會在 `server.go` 加路由；共用樹上同時改同一個檔
會互相蓋掉。各自在 worktree 改，root 依序合併、解衝突、統一重生契約。

每個 child 用自己的 daemon 與 port，不碰 7717、7727：

| child | port | 標題 |
|---|---|---|
| A | 7761 | 圖片：附圖、貼上、拖拉、上傳、對話裡的圖 |
| B | 7762 | 語音輸入：錄音 → `/v1/voice` → whisper-cli |
| C | 7763 | 在 Mac 上顯示＋即時畫面 |
| D | 7764 | Git 面板＋commit／push |
| E | 7765 | 文件＋我傳出的訊息 |
| F | 7766 | 在瀏覽器回答問題：等待卡、`menu`、`/key` |

### 共用前言（每份派工都放在最前面）

- clawdline-go（Go 核心＋React console）正在「嚴格復刻」舊 Swift app。舊版 `http://127.0.0.1:7717/`（瀏覽器已配對；
  舊版的 `/v1/*` 要在 7717 的分頁裡 fetch）。先讀 `docs/replica.md`、`docs/plan.md` §4、`docs/remote.md`、
  `web/console/src/legacy/README.md`、本文件。
- 你在隔離 worktree 裡。**不要重啟或重建 7727。** 自己的 daemon：
  `go build -o bin/clawdline ./cmd/clawdline`，然後
  `CLAWDLINE_NEXT_PORT=<你的 port> CLAWDLINE_NEXT_DIR=<你的暫存目錄> CLAWDLINE_NEXT_OWN_SESSIONS=1 CLAWDLINE_NEXT_STANDALONE=1 CLAWDLINE_NEXT_WEB=$PWD/web/console/dist ./bin/clawdline serve`。
  前端要先接上 node_modules：`web/node_modules` 建成實體目錄，把 `~/code/clawdline-go/web/node_modules` 底下
  除了 `@clawdline` 以外的項目逐一 symlink 進來，`@clawdline/{console,contract,core}` 用相對 symlink 指向
  `../../console` 等（**不可以**整個 symlink 到共用樹，否則型別會解析到別人的檔案）；`web/console/node_modules`
  可以直接 symlink。然後 `cd web/console && npm run build`。
- 若 master 已合進認證（`free-auth`），daemon 需要認證：瀏覽器用 `clawdline open --print`（同樣的環境變數），
  curl 帶本機 token。見 `docs/replica.md` 的量測段落。
- 慣例：舊版 CSS／JS 逐位元組照抄到 `web/console/src/legacy/`（不可修改；新增要更新 `MANIFEST.json`，
  `tools/check-legacy-css.sh` 必須 exit 0）；React 只產出與舊版相同的 DOM 與 class；文字經 `legacy/bridge.ts`
  或你自己的 `legacy/<name>-bridge.ts`；字串只用 `zh-Hant.json` 已有的鍵。契約：新增或修改 `api/v1/*.schema.json`，
  在你的 worktree 跑 `go run ./tools/contract-gen` 與 `-check`。
- 量測沿用 reviewer 的方法（`/tmp/.clawdline/618bd064-2d80-4367-a68e-b9ae365ee4f6/artifacts` 的 measure2.js、
  diff2.mjs）：同一分頁輪流載入 7717 與你的 port，比 computed style、rect、文字、屬性；行為要實際操作。
- **不可以動 `~/code/clawdline`**，不可以寫 `~/.config/clawdline`，不可以讀任何 token 或 secret。
  **不可以對使用者正在用的 session 送訊息、中斷、關閉、commit 或 push。** 需要 session 時，開一個可拋棄的
  （在 `/tmp` 下的目錄開新分頁跑 assistant，測完關掉），或用 fake terminal host。
- 不要 git commit（root 驗收後提交）。額度緊：build 與測試只在驗收時各跑一次。沒量到的要明說，不可當作通過。
  進度寫進 `artifacts/report.md`；summary 列出你改了哪些共用檔（`server.go` 的確切行、`Composer.tsx`／
  `Detail.tsx` 改了哪一段）。
- 跨平台：Go 的部分要能在 Linux 與 Windows 編譯；只有 macOS 才有的東西放在 build tag 或 `shell/darwin/`，
  其他平台回具名的「不支援」錯誤，不要默默失敗。

### A 圖片

舊版：`input/shots.js`、`view/transcript-images.js`、`index.html` 的 `#shots`、`#pick`、`#attach`；
`RemoteServer.swift` 的 `/v1/artifacts/images`（存與讀）與送出時帶圖的路徑（找出圖片怎麼交給 terminal 裡的
assistant）；拖拉在舊版接在哪個元素上、接受哪些型別與大小。
Go：自己的圖片儲存（`CLAWDLINE_NEXT_DIR` 下，0700／0600，大小與型別上限），讀圖路由，送出帶圖。
對話裡已經存在的 `<clawdline-image>` 標記指向舊 app 的 artifact 儲存：照 `plan.md` §4 的規則唯讀地讀來顯示，
寫進 §4 的清單。前端：附圖按鈕、檔案挑選、貼上圖片、拖拉、縮圖列、移除、錯誤訊息，全部照舊版。
可改：`web/console/src/session/Composer.tsx`、`web/console/src/session/Transcript.tsx`（只改圖片那段）、
`web/console/src/legacy/`（新增照抄檔與 `shots-bridge.ts`）、`internal/transport/http/`（新增 `images.go`、
`actions.go` 的送出帶圖、`server.go` 的路由行）、`internal/adapters/artifacts/`（新）、`internal/adapters/swiftstore/`（只新增檔）、
`internal/app/actions.go`、`api/v1/`、`docs/plan.md`（§4 清單）。

### B 語音輸入

舊版：`input/voice.js`（瀏覽器用 MediaRecorder 錄音，轉成 PCM 上傳，不是直接傳錄音檔）、`net/live.js` 的 `voice()`、
`RemoteServer.swift` 約 3782 行的 `transcribe`、`Whisper.swift`（找 `whisper-cli` 與 `ggml-*.bin` 的路徑、詞彙表、
逾時）、`index.html` 的 `#mic`、`#voice`。
Go：`POST /v1/voice` → 執行 `whisper-cli`。搜尋路徑要涵蓋 macOS（Homebrew）、Linux、Windows；同時只跑一個，
有大小與時間上限，暫存檔一定清掉，錯誤具名（沒裝、沒模型、逾時、太大）。**這台機器若沒裝 whisper-cli，
不要自己安裝**，只驗「沒裝」的路徑，並在 report 寫明。
原生殼：WKWebView 要允許麥克風（`WKUIDelegate` 的媒體權限、`Info.plist` 的 `NSMicrophoneUsageDescription`，
打包腳本 `tools/package-macos.sh` 要帶上）。
可改：`web/console/src/session/Composer.tsx`（只改 `#mic`、`#voice` 那段）、`web/console/src/legacy/`（照抄
`voice.js` 與 `voice-bridge.ts`）、`internal/transport/http/voice.go`（新）、`server.go` 的路由行、
`internal/adapters/whisper/`（新）、`api/v1/`、`shell/darwin/`、`tools/package-macos.sh`。

### C 在 Mac 上顯示＋即時畫面

舊版：`RemoteServer.swift` 的 `/focus`、`/screen` handler 與它們用到的 terminal 程式（iTerm2、Terminal、tmux 各自
怎麼做）；前端 `#tx-focus`、`#session-focus`、`#session-screen`、`#screen-panel`、`#screen-badge` 與對應的 js。
記住：Ghostty 驅動得動但讀不到畫面，舊版怎麼處理就怎麼處理。
Go：`internal/app/ports` 的 TerminalHost 加上 focus 與讀畫面；各 terminal 實作在 `internal/adapters/terminal/`。
tmux 在 Linux 也能用；沒有對應實作的平台回具名的不支援。
測試：「在 Mac 上顯示」會把視窗叫到前面，只對你自己開的可拋棄 session 做，做一兩次就好。
可改：`web/console/src/session/Detail.tsx`（只改這三個入口與 screen panel）、`web/console/src/session/`（新增
`ScreenPanel.tsx`）、`web/console/src/legacy/`、`internal/transport/http/`（新增 `focus.go`、`screen.go`，
`server.go` 或 `/v1/sessions/` 的分派）、`internal/app/`、`internal/adapters/terminal/`、`api/v1/`。

### D Git 面板＋commit／push

舊版：`input/git-panel.js`、`#git-panel`、`#session-git`、`#session-commit`、`#session-push`（`data-action`）、
`RemoteServer.swift` 的 `/git`；commit 與 push 在舊版到底做什麼（是送一句話給 assistant，還是 daemon 自己跑 git），
以及確認框（新版已有 `overlays/`，`requestConfirm`）怎麼接。
Go：`/v1/sessions/<id>/git`，用既有的 `internal/adapters/git`。
測試：**只在你自己建的可拋棄 repo 與可拋棄 session 上做 commit／push**；push 的 remote 用本機的 bare repo。
可改：`web/console/src/session/Detail.tsx`（只改 Git 子選單三項）、`web/console/src/session/`（新增
`GitPanel.tsx`）、`web/console/src/legacy/`、`internal/transport/http/`（新增 `git.go`）、`internal/adapters/git/`、
`internal/app/`、`api/v1/`。

### E 文件＋我傳出的訊息

舊版：`view/documents.js`、`document-render.js`、`document-links.js`、`#documents-page` 與相關元素、
`RemoteServer.swift` 的 `/documents` 系列（session 的、project 的）；我傳出的訊息是 `input/user-messages.js`
與已照抄的 `view/user-messages-data.js`。
Go：文件路由。**這是讀檔的路由，路徑穿越是頭號風險**：照舊版的範圍規則，拒絕 `..`、symlink 跳出、絕對路徑，
並用實際請求驗證。
可改：`web/console/src/session/Detail.tsx`（只改這兩個入口）、`web/console/src/pages/`（新增 documents 頁或 sheet，
照舊版的掛法）、`web/console/src/session/`（新增 user messages sheet）、`web/console/src/legacy/`、
`internal/transport/http/`（新增 `documents.go`）、`internal/adapters/documents/`（新）、`api/v1/`。

### F 在瀏覽器回答問題

舊版：`view/composer.js` 的等待卡（`#waiting`：問題、選項按鈕、多選、送出、步驟、`answeredMenu` 的「已送出」狀態、
`menuKey` 的「揮掉這個選單」）；session 列的 `menu`（`RemoteServer.swift` 約 5353 行的 `menuObject`，以及它從畫面
解析選單的程式，找出是哪個檔）；`POST /v1/sessions/<id>/key`（`ITerm.keystroke`、`Tmux.keystroke` 送一個原始位元組，
不包 bracketed paste）。舊版的註解記錄過：用 `/send` 回答選單會送錯選項（"Tea" 變成 "Water"），所以**回答只能走 `/key`**。
Go：`internal/adapters/terminal` 加 keystroke；讀畫面解析 `menu`，放進 `/v1/sessions` 的列（`inventory.go` 已經會讀畫面）；
`/key` 路由要求 write 權限，只接受舊版允許的按鍵。前端照舊版畫等待卡。
測試：**只用可拋棄的 session**：在 `/tmp` 下開一個新分頁跑 claude，請它用 AskUserQuestion 問一個無害的問題，
從你的 port 的頁面按選項回答，確認畫面上選到的是你按的那一個；單選、多選各一次。不要對使用者的 session 按任何鍵。
可改：`web/console/src/session/Composer.tsx`（只改 `#waiting` 那段）、`web/console/src/session/`（新增 `Waiting.tsx`）、
`web/console/src/legacy/`、`internal/transport/http/`（新增 `keys.go`、`sessions.go` 的 `menu`）、`internal/app/`、
`internal/adapters/terminal/`、`api/v1/`。與 C 都會改 `internal/adapters/terminal/`，由 root 合併。

## 整合（root）

依完成順序合併；`Composer.tsx`、`Detail.tsx`、`server.go`、`bridge.ts`、`MANIFEST.json` 的衝突由 root 解。
每合一個：重生契約並 `-check`、`go vet`／`build`、`tsc`、`check-legacy-css.sh`，提交，landing。
六個都合完後，重建 7727，派一個 reviewer 審這六項。
