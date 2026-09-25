# 一個不是他的人第一次打開 Clawdline 會撞到什麼

2026-09-21 的盤點。**這是一份派工清單，不是修正紀錄**：這一輪沒有改任何產品程式碼。

到今天為止，這個 repo 幾乎每一個缺陷都是作者本人撞到才被發現的。這一輪換一個人走：一個新帳號、
一台新機器、一個新瀏覽器，照著 `docs/getting-started.md` 一步一步做，把每一步看到的字記下來。

## 0. 先讀這段

- **一共 91 筆**：擋住 14、誤導 48、粗糙 29。其中 16 筆有實地跑出來的證據（〔實測〕），其餘是讀程式碼確認的（〔讀碼〕）。
  讀碼的行號都用 `sed -n`／`grep -n` 核對過。
- **最先會撞到的一件事，發生在第一行指令。** 文件叫人 `git clone https://github.com/sainteye/clawdline.git`，
  那個公開 repo 現在放的是已退役的 Swift app；`github.com/sainteye/clawdline-go` 不存在。新使用者連程式碼都拿不到（B01）。
- **91 筆全部可以歸到 10 個成因。** 同一個成因的派成一件事，不要一筆一筆派。最大的三個：
  1. **「不知道」在路上被換成一個確定的字**（U4，23 筆）：daemon 知道原因，但原因沒送上 wire，
     或送上了畫面不讀，最後變成 `0`、`沒有 session`、`在線`、`畫面讀不到`、`在等`。
  2. **人就在機器前面，畫面卻不說下一個指令**（U2＋U3，14 筆）：配對碼要去哪裡看、怎麼讓瀏覽器能打字、
     帳號裡還沒有機器時要跑什麼，畫面都沒說。這些指令全在 CLI 裡，畫面一個也沒提。
  3. **舊 app 的字典和頁面原封不動搬過來了**（U6，16 筆）：畫面文字是舊 Swift app 的逐位元組複本，守衛刻意不讀它。
     所以 Linux 上也叫「那台 Mac」，整頁是英文；頁面還在承諾這個 daemon 根本不產生的東西。
- **那 4 件要他拍板的事，2026-09-21 全部拍板了**（§5）。
- **有 13 個狀態沒走到**，每一個都在 §2 寫了理由。

## 1. 怎麼走的

我沒有開新帳號，也沒有碰他的 Cloud 或 `~/code/clawdline`。對 7727 只送過任務協定要求的 `accepted` 與 `inflight` 兩個請求。
B01 用的是對 GitHub 的唯讀查詢（`git ls-remote`、`gh api`）。

- **編譯**：照 getting-started §1 在這個 worktree 裡跑。`npm install`、`npm run build`、`go build` 都一次成功，
  因為這台機器已經有 Go 1.27 和 Node 24。
- **自己起一個 daemon**：port 7791，搭配四個隔離設定：
  - 空的 `CLAWDLINE_NEXT_DIR`
  - 空的 `HOME`（沒有 `~/.claude`、`~/.codex`）
  - 只屬於這個 daemon 的 tmux socket（`TMUX_TMPDIR`）
  - `CLAWDLINE_NEXT_LEGACY_STORE=off`、`CLAWDLINE_NEXT_RECLAIM=off`

  daemon 都是用它自己的 PID 收掉的。
- **瀏覽器**：一個 Chrome 分頁，只開過 `127.0.0.1:7791`。先不帶 token 打開（看到配對門），再用
  `clawdline open` 的唯讀連結登入。
- **第一個 session**：在我那個 tmux 裡，用空的 `HOME` 跑 `codex`。它停在自己的首次登入畫面，
  這正是新使用者的第一個 session 會長的樣子。
- **CLI**：`doctor`、`open`、`pair`、`tunnel`、`cloud status|preflight|devices|commands on` 都跑過。
  會連正式服務的指令（`cloud login`、`cloud pair`）一律沒跑。
- **讀程式碼**：拆成三塊，交給三個唯讀的內建 subagent 平行讀：hosted console、本機 console、CLI 與 daemon。
  它們回報的擋住等級宣稱，我挑了幾筆自己核對：`ChoosePlan` 的 auto 分支、`WRITE_OFF`、
  `machineShortID` 的 `slice(-8)`，並實際重現了 tmux 不在 `PATH` 的情況（B03）。

證據截圖：`artifacts/first-codex-session-detail.png`（不在 repo 裡）。圖裡是第一個 session 的詳細頁，對應 M10、M11、M24。
畫面上只有假的 `my-app`，沒有他的資料。

## 2. 沒走到的，與理由

| 狀態 | 沒走到的理由 | 用什麼代替 |
| --- | --- | --- |
| 在手機上打開 app.clawdline.com、選機器、送訊息 | 任務規則禁止開 app.clawdline.com（會和他手機共用 device identity）；帳號 API 在另一個 repo，本機沒有替身服務，hosted client 是複製來的檔案（`web/console/src/cloud/copied.ts`） | 讀碼（U3、U4-C、U10） |
| `cloud login`、`cloud pair` 對正式服務 | 會在他的 Cloud 帳號留下註冊或配對紀錄 | 讀碼（U8） |
| 第二台機器加進同一個帳號 | 同上，需要 Cloud | 讀碼（U10） |
| macOS app 第一次開 | 要用 swiftc 打包，這台機器 swap 很滿；而且 app 的 daemon 會去搶 7727 | 讀碼（U9） |
| 零個 session 的空清單（實機畫面） | 這台 Mac 的 iTerm2 開著，daemon 一定會列出他本人的 iTerm2 session，沒有開關能排除 | 讀碼（M34、R23） |
| 沒裝 tmux | 啟動器會到固定目錄找 tmux，沒辦法藏起來；我只模擬了「tmux 不在 `PATH`」 | 實測 B03＋讀碼 |
| 沒有 iTerm2，或 macOS 沒允許自動化 | 不能在他的機器上解除安裝 iTerm2 或撤銷權限 | 讀碼（B07） |
| 只用 Terminal.app／VS Code 終端機的人 | 我能啟動的每個 process 都是 iTerm2 的後代，daemon 把我開的 pty 標成 `backend: iterm` | 沒有代替，**這一項目前未知** |
| 沒裝 claude／codex | 空 `HOME` 只能模擬「沒有紀錄」，模擬不了「沒有執行檔」 | 讀碼（B06） |
| 沒有網路 | 沒有斷網 | 讀碼（M20、M41） |
| 在唯讀分頁按送出 | 清單裡是他本人的 live session，萬一權限檢查放行，就會打字進他的終端機 | 讀碼（B08）；我只確認了輸入框上沒有任何唯讀說明 |
| 從 console 開一個新 session | 會在他的 Mac 上開一個終端機分頁 | 只打開面板讀內容、不送出（B06）；其餘讀碼（B05、M48） |
| Linux | 沒有 Linux 主機 | 讀碼；另見 in-flight `31c4bd21`（§6） |

## 3. 派工單位：同一個成因就派成一件事

| 單位 | 成因（一句話） | 發現 | 最重 | 和 in-flight 的關係 |
| --- | --- | --- | --- | --- |
| **U1** 發行與文件 | 拿不到程式碼；照著做會做錯；讀到的是作者本人那台機器的東西 | B01 B02 M01 M02 M03 R01 R02 | 擋住 | `31c4bd21`（Linux 從頭跑通）改的是 `docs/linux.md`，沒有重疊 |
| **U2** 本機：人在機器前卻不知道下一個指令 | console 不知道自己的權限（health 不帶 `send`），門口和輸入框不提 CLI | B04 B08 B14 M04 M05 M06 M07 M08 | 擋住 | 無 |
| **U3** hosted：第一台機器與配對 | 帳號、瀏覽器、機器三者的關係沒有一個畫面講得清楚 | B09 B10 B11 B12 B13 R03 | 擋住 | **`fd2d006e`（PWA 裡配對新機器）正在做 B09、B11 的一部分** |
| **U4** 「不知道」要帶著原因走到畫面 | 原因在 daemon→wire→畫面的某一段被丟掉，換成 `0`、空、在線、`畫面讀不到` | A daemon/wire：B03 B07 M09 M10；B 本機 console：M11–M18 R04–R07；C hosted：M19–M23 R08 R09 | 擋住 | `cf0e4585`（額度 unknown 說出為什麼）是同一家族，已交付未落地 |
| **U5** 名字 | 名字的最後一階是 id、tty、UUID | M24 M25 R10–R14 | 誤導 | `69e8e237`（剛開的 Codex 沒有身分）可能改到 M24 的上游 |
| **U6** 舊 app 的字典與移植頁 | 字典逐位元組鎖定，守衛不讀；頁面搬過來了，但產生資料的那一端沒有 | a 字：M26 R15–R21；b 頁：M27–M33 R22 | 誤導 | `620c9aee`（驗證帳本與專案時間軸）可能改到 M28、M29；`16104842`（還在照舊 app 規則的地方）同一家族 |
| **U7** 第一個 session 從哪裡來 | daemon 早就知道有沒有 tmux、iTerm2、assistant（`/v1/diagnostics`、`installedAssistants`），console 和 doctor 都不讀 | B05 B06 M34 M35 M48 R23 R24 R25 | 擋住 | `f47994be`（語音）會動到 R24 的 adapter |
| **U8** Cloud CLI：設定檔對上正在跑的 daemon | CLI 只讀寫設定檔；daemon 只在啟動時讀開關；兩邊從不對帳 | M36–M42 R26 R27 | 誤導（M36 涉及安全） | 無 |
| **U9** daemon 與 app 的啟動 | 啟動失敗或被佔用時，給的是錯的補救 | M43 M44 M45 R28 | 誤導 | 無 |
| **U10** hosted：第二台機器 | 帳號上的名字和清單上的名字是兩個來源；換機器的入口藏起來了 | M46 M47 R29 | 誤導 | 無 |

每一筆只屬於一個單位。筆數：U1 7、U2 8、U3 6、U4 23（A 4、B 12、C 7）、U5 7、U6 16（a 8、b 8）、U7 8、U8 9、U9 4、U10 3，合計 91。
B03（tmux 不在 `PATH`）也會影響 U9 的 Mac app，但算在 U4。

### 各單位的總驗收

每個單位都附一個「整類」的紅燈條件。修完之後，這一類不會只剩某一種寫法被擋住。

- **U1**：在一個乾淨的容器裡，只照 `docs/getting-started.md` 逐字執行，最後 `GET /` 回 200，
  清單出現一個 session。今天在第一行 `git clone` 之後就走不下去。
- **U2**：`clawdline open`（不帶 `--send`）打開的分頁，在按下任何會被拒的按鈕**之前**，畫面上就有
  `clawdline open --send`。沒有任何處理配對的畫面時，門口也會出現 `clawdline open` 或 `clawdline pair`。
- **U3**：一個帳號 0 台機器的瀏覽器登入後，畫面有安裝與 `clawdline cloud login` 的步驟。
  一列 `not_paired` 的機器至少有一個能按、而且不是 ✎/× 的按鈕。
- **U4**：一個掃過所有計數與狀態渲染點的測試。餵給它「沒有 snapshot」或「來源不完整」，任何地方都不可以出現
  `0`、`沒有`、`在線`、`畫面讀不到`；餵給它帶 `unknown_reason` 或來源失敗原因的 snapshot，
  畫面上要出現那個原因對應的句子。分三片派（A daemon/wire、B 本機 console、C hosted console），共用同一條規則和同一個守衛。
- **U5**：清單、標題、確認鈕、CLI 輸出上，不可以出現 8 碼以上的十六進位、UUID、`ttysNNN` 或 `%N`
  當作主要名字。它們只能出現在「細節」裡。
- **U6**：`check-machine-words.sh` 也掃畫面實際吃到的字串（不只 `.ts`／`.tsx`），zh 介面下 DOM 不含英文句子。
  每個側欄頁宣稱的資料都要有一個 producer 的測試，沒有 producer 的頁面就不顯示。
- **U7**：在「tmux 有裝、沒有 server、沒有 iTerm2」的機器上，console 的 ＋ 可以開出一個 session；
  `doctor` 在沒有 tmux 時 exit 非 0，並說出來。
- **U8**：每個會改開關的 `cloud` 子指令結束時，都印出**正在跑的 daemon**的狀態；兩邊不一致時，要說出要重啟。
- **U9**：佔住 port 的是自己的 daemon 時，不再建議「換一個 port」；app 的 daemon 結束時，視窗說出 exit status 和 log 位置。
- **U10**：在帳號上改名之後，機器清單顯示新名字；在 A 的畫面按 B 的「開新 Session」，會開在 B 上。

## 4. 發現清單（按嚴重度）

每筆的格式：**在哪**（哪一頁、按了什麼）／**原字**（當下螢幕上的字）／**為什麼幫不了**／**位置**／**應該**
（應該說什麼、做什麼）／**紅燈**（一句會紅的驗收）。標題後面是證據種類和所屬單位。

### 4.1 擋住（走不下去）

#### B01〔實測〕clone 下來的是已退役的 Swift app｜U1
- **在哪**：README「Install and run」或 getting-started §1，照抄第一行指令。
- **原字**：`git clone https://github.com/sainteye/clawdline.git ~/code/clawdline`
- **為什麼幫不了**：那個公開 repo 現在的根目錄是 `Package.swift`、`Sources/`、`build.sh`、`test.sh`，最後一個 commit 是
  2026-09-16。repo 描述還寫著「on your Mac … iTerm2 or tmux」。那裡沒有 `web/`，也沒有 `cmd/clawdline`，所以下一行的
  `(cd web && npm install && npm run build)` 和 `go build ./cmd/clawdline` 都會直接失敗。`github.com/sainteye/clawdline-go`
  回的是 `Repository not found`，這個 checkout 本身也沒有設 remote。
- **位置**：`docs/getting-started.md:25`、`README.md:167`；`go.mod:1`（`module github.com/sainteye/clawdline-go`）
- **應該**：先決定公開的位置（§5 D1），再讓文件、`go.mod`、repo 描述三者一致。
- **紅燈**：在一個乾淨的容器裡，照 `docs/getting-started.md` 第 25 行 clone 下來之後，`ls web/package.json cmd/clawdline/main.go` 兩個檔案都要存在。今天兩個都不存在。

#### B02〔讀碼〕沒寫 Node 要哪一版，舊 Node 在 Vite 深處爆掉｜U1
- **在哪**：getting-started §0 的表格，接著 §1 的 `npm install`。
- **原字**：`builds the console. No version is pinned yet`
- **為什麼幫不了**：Vite 6 要求 `^18.0.0 || ^20.0.0 || >=22.0.0`。用發行版套件管理器裝到舊 Node 的 Linux 使用者，
  會在 Vite 裡面看到一段語法錯誤。文件和 `web/package.json` 都沒有先講這件事。
- **位置**：`docs/getting-started.md:14`；`web/package-lock.json:1829-1831`；`web/package.json`（沒有 `engines` 欄位）
- **應該**：文件寫「Node 18、20 或 22 以上」；`web/package.json` 加上 `engines.node`，並設 `engine-strict`。
- **紅燈**：在 Node 16 上跑 `npm install`，第一行錯誤要說出版本需求。今天不會。

#### B03〔實測〕tmux 不在 `PATH` 時，tmux 裡的 session 從清單消失，而且清單還宣稱「讀完了」｜U4-A
- **在哪**：從 Finder 打開 Mac app（launchd 給的 `PATH` 沒有 `/opt/homebrew/bin`），或在 `PATH` 比較短的 shell 裡跑 `serve`。
- **原字**：畫面上什麼都沒有，那個 session 不在清單裡。`/v1/sessions` 回 `sources: tmux complete=true`、沒有任何 gap；
  只有 `/v1/diagnostics` 裡寫著 `tmux is at /opt/homebrew/bin/tmux, which is not on this daemon's PATH, and the tmux backend runs \`tmux\` from the PATH`。
- **為什麼幫不了**：我用 `PATH=/usr/bin:/bin:/usr/sbin:/sbin` 啟動自己的 daemon，tmux 裡正在跑的 codex 就從清單消失了，
  掃描還是回「完整」，所以「看不到」被當成「確定沒有」。啟動器會到固定的幾個目錄找 tmux，清單卻只看 `PATH`。
  文件說那幾個目錄都會找，這句話只對了一半。
- **位置**：`internal/adapters/terminal/tmux.go:72-74`；`internal/adapters/terminal/capabilities.go:57-61`；`internal/adapters/terminal/launch.go:40-49`；
  `shell/darwin/main.swift:397`；`docs/getting-started.md:216`
- **應該**：清單和驅動都改用啟動器的查找順序。真的找不到時回 `Complete=false`，附一個 gap「tmux not found on PATH or in …」，並顯示在畫面上。
- **紅燈**：用 `PATH=/usr/bin:/bin` 啟動 daemon，tmux 只放在 `/opt/homebrew/bin`，tmux 裡的 session 仍要出現在 `/v1/sessions`。今天不會出現。

#### B04〔實測〕配對門：配對碼哪裡都不會出現，門口也從沒提到 `clawdline open`｜U2
- **在哪**：跑完 `serve` 之後，直接在瀏覽器打 log 裡的 `http://127.0.0.1:7727`，按「要求配對」。
- **原字**：`這個瀏覽器還沒被放進來。`／`按下要求配對，六位數字會出現*在那台 Mac 上*——它刻意不放在給這一頁的回應裡。只有人在那台機器旁邊才完成得了，這正是重點，不是刁難。`
  按下去之後是：`看那台 Mac。`／`Clawdline 正在顯示六位數字，還有 2:00。打在這裡。`
- **為什麼幫不了**：只有兩個地方會顯示配對碼：Mac app，或正在跑的 `clawdline pair`。只跑了 `serve` 的人，兩個都沒有。
  我按下去之後，daemon 的終端機和 log 都沒有任何動靜，所以「Clawdline 正在顯示六位數字」這句話是錯的。
  人明明就在這台機器前，最短的路是 `clawdline open --send`，門口一個字都沒提；Linux 使用者還被叫去看「那台 Mac」。
  `serve` 啟動時的 listening 那行也沒有給下一步，括號裡寫的是搬遷時期的用語：
  `clawdline-go listening on http://127.0.0.1:7727 (nothing behind it: an unowned route answers 501 not_implemented and names itself)`。
- **位置**：`web/console/public/strings/zh-Hant.json:211`、`:212`、`:215`、`:216`；`web/console/src/door/Door.tsx:201-202`、`:214-219`、`:469`；
  `internal/transport/http/auth.go:111-134`（`beginPairing` 不知道有沒有人在聽）；`internal/transport/http/server.go:620`
- **應該**：在 loopback 上加一句：「你就在這台機器上：在終端機執行 `clawdline open --send`，會直接開一個已登入、能打字的分頁。」
  配對請求的回應要帶「現在有幾個地方在顯示代碼」，是 0 的話就說「先執行 `clawdline pair --watch`，或打開 Clawdline app」。
  daemon 收到配對請求時，在 log 寫一行（不寫代碼）。listening 之後印出下一步：`open the console: clawdline open`。
- **紅燈**：沒有任何 `/v1/auth/pairings` 監聽者時按「要求配對」，門口要出現 `clawdline open` 或 `clawdline pair`。今天兩個都沒有。

#### B05〔讀碼〕全新機器按 ＋ 開 session，被叫去開 iTerm2（Linux 也一樣）｜U7
- **在哪**：設定檔沒寫 `terminal`，有裝 tmux 但一個 tmux 都還沒開，也沒有開著的 iTerm2（Linux 永遠是這樣）→ 清單右上的 ＋ → 選一個專案。
- **原字**：`iTerm2 沒有在 Mac 上跑。先在那邊打開它，再試一次。`
- **為什麼幫不了**：`auto` 模式遇到「tmux 有裝、但沒有 server」時回的是 `PlanNotRunning`，不會用 `PlanTmuxDetached` 自己起一個 server。
  使用者明確選了 tmux 的話，daemon 其實會這樣做。
- **位置**：`internal/adapters/projects/launch.go:282-288`；`internal/app/start.go:110-115`；`web/console/src/session/Start.tsx:160`；`zh-Hant.json:739`
- **應該**：`auto` 在 tmux 有裝時走 `PlanTmuxDetached`。兩個都沒有才拒絕，並說「這台機器沒有 tmux（也沒有開著的 iTerm2）。裝好 tmux 再按一次。」
- **紅燈**：`ChoosePlan(TerminalAuto, false, TmuxInstalled)` 要回 `PlanTmuxDetached`。今天回 `PlanNotRunning`。

#### B06〔實測＋讀碼〕從沒在任何目錄工作過的機器，在 console 裡開不出第一個 session｜U7
- **在哪**：清單右上的 ＋（「在某個專案裡開一個 session」）。
- **原字**：面板只有 `要在哪裡開？`、`要用哪一個開？`，加一個 `篩選專案` 搜尋框。清單是空的時候：`還沒有地方可以開。在 Mac 上跑過一次 Claude Code，它就會出現在這裡。`
- **為什麼幫不了**：只能從歷史紀錄裡挑目錄，沒有地方輸入路徑（我打開面板看過，只有那一個篩選框）。
  那句話沒提 Codex，但 Codex 的紀錄其實也會被讀。`claude` 和 `codex` 都沒裝時，選擇列會被藏起來，所以看不出來是沒裝；
  這時按下任何一個地點，還是會用 `claude` 啟動，最後只看到 `分頁已經開了，但那個 session 還沒回報。去 Mac 上看一下。`
- **位置**：`zh-Hant.json:727`；`web/console/src/session/Start.tsx:202`、`:337`、`:454-456`；`web/console/src/legacy/start-bridge.ts:129`；`internal/transport/http/projects.go:152-171`（`installedAssistants`）
- **應該**：給一個可以輸入路徑、或至少可以選家目錄的入口。空的時候說「在 tmux 裡 `cd` 到專案，跑一次 `claude` 或 `codex`」；兩個都沒裝時直接說出來。
- **紅燈**：`/v1/places` 回 `places:[]`、`assistants:[]` 時，面板要有一個可以輸入路徑的欄位，而且要提到 `codex`。今天兩者都沒有。

#### B07〔實測＋讀碼〕某個來源讀不到時，清單永遠「在等」，原因在 daemon 裡就被丟掉｜U4-A
- **在哪**：Mac 上 iTerm2 開著，但沒允許自動化（或 Apple Event 逾時）；或 tmux 回了「no server running」以外的錯誤。
- **原字**：清單顯示 `在等這台機器的清單`／`線已經通了。這台機器還沒說它現在有哪些 session。`；頁首一直掛著 `在等這台機器的清單`，
  而且清單已經畫出來了，這句也不會消失。
- **為什麼幫不了**：我實測過一次 tmux 失敗（socket 路徑太長）。`/v1/sessions` 的 `sources` 只回 `{"source":"tmux","complete":false}`，
  log 裡沒有任何一行提到 tmux，畫面就一直說「在等」。來源的結果在 inventory 裡只是一個 `map[string]bool`，錯誤在這裡就沒了。
  iTerm2 那一條路也一樣：使用者真正該去的地方是「系統設定 → 隱私權與安全性 → 自動化」，但沒有任何地方會告訴他。
- **位置**：`internal/app/inventory.go:80-95`；`internal/transport/http/sessions.go:211-222`（`scanSources`）、`:184`；
  `internal/adapters/terminal/iterm_darwin.go:142-143`；`web/console/src/Sessions.tsx:155`；`web/console/src/App.tsx:891`；`web/console/src/next-strings.ts:216-217`
- **應該**：`Sources` 帶上每個來源的失敗原因（錯誤種類和一句話），寫進 log，畫面依原因說出來，例如「讀不到 iTerm2：macOS 沒有允許 Clawdline 控制它……」。
- **紅燈**：讓 tmux 來源回任何一個錯誤，`/v1/sessions` 的 `scan.sources[tmux]` 要帶非空的原因，畫面也要出現 `tmux`。今天只有 `complete:false`。

#### B08〔實測＋讀碼〕唯讀分頁：輸入框照樣能打字，送出才被拒，「再試一次」永遠失敗｜U2
- **在哪**：用 getting-started §3 的 `clawdline open`（沒加 `--send`）打開的分頁 → 點一個 session → 打字 → 送出。
- **原字**：輸入框上的提示是 `跟 Codex 說⋯`，畫面上沒有任何「只能看」的字。送出之後，卡片顯示 `送不出去（forbidden）`，旁邊是 `再試一次`。
- **為什麼幫不了**：`/v1/health` 只回 `authed`，不回有沒有打字權限，所以輸入框預設就是開的。`forbidden` 不在會關掉輸入框的代碼清單裡。
  全站沒有任何地方寫 `clawdline open --send`，而新使用者開 console 的第一件事，就是想打字。
- **位置**：`internal/transport/http/server.go:444`；`web/console/src/session/Composer.tsx:66-68`、`:84`、`:407-417`；`web/console/src/session/outcome.ts:68-74`；
  `web/console/src/session/Transcript.tsx:291-298`；`zh-Hant.json:26`、`:530`；`cmd/clawdline/auth.go:137-140`
- **應該**：health 帶上這個憑證的 `send`。唯讀時，輸入框一開始就關著，並說「這個分頁只能看。要能打字，在這台機器執行
  `clawdline open --send`，改用新開的分頁。」daemon 對唯讀裝置回一個獨立的代碼，把它加進 `WRITE_OFF`。
- **紅燈**：唯讀裝置打開任何一個 session，輸入框要是關的，而且頁面上要有 `clawdline open --send`。今天輸入框是開的，全站也找不到這個字串。

#### B09〔讀碼〕hosted：整個 PWA 沒有配對入口，機器那邊卻叫人到手機上按｜U3（in-flight `fd2d006e`）
- **在哪**：登入 app.clawdline.com，但這個瀏覽器還沒有帳號金鑰。或者用相機掃機器設定頁上的配對 QR、打開 `clawdline cloud pair` 印出的連結。
- **原字**：手機上：`已登入，但還沒配對`／`這個瀏覽器沒有帳號 {account} 任何一台機器的金鑰。這一版還不能在這裡配對：請在那台機器上完成配對，再重新整理這一頁。`
  機器設定頁上：`按「Scan the QR on the Mac」，對準這張 QR。`
- **為什麼幫不了**：兩邊互相叫對方去做，形成死循環。
  - 手機上那顆「Scan the QR on the Mac」按鈕，在 React 的 hosted 版裡根本不存在。
  - `#pair=` 片段會被丟掉兩次：OAuth 的 `returnTo` 丟一次，manifest 的 `start_url` 再丟一次。
  - `pairing_required` 之後，檢查迴圈就停了；iOS 主畫面 app 也沒有重新整理這個動作。
- **位置**：`web/console/src/next-strings.ts:207-208` → `web/console/src/cloud/CloudGate.tsx:834-840`；`web/console/src/legacy/js/net/cloud-boot.js:233`、`:625`、`:1120-1122`；
  `web/console/src/legacy/js/net/cloud-onboarding.js:55`；`web/console/public/manifest.webmanifest`；`web/console/src/pages/settings/window/copy.ts:182`、`:193-194`
- **應該**：配對畫面要有三樣東西：「掃機器上的 QR」、「貼上配對連結」、顯示本機的 pairing code。進頁面時讀 `#pair=`，並在 OAuth 來回之間保存下來。
  完成後自動重新接上，另外給一顆「重新檢查」按鈕。
- **紅燈**：已登入、沒有帳號金鑰的瀏覽器開 `/#pair=<有效邀請>`，要進入指紋確認畫面，而不是 `data-cloud-screen="pairing"`。今天停在 pairing，卡片上也沒有任何按鈕。

#### B10〔讀碼〕hosted：帳號裡一台機器都沒有，被當成「還沒配對」，也沒說要在機器上跑什麼｜U3
- **在哪**：全新帳號第一次登入。
- **原字**：`請在那台機器上完成配對`；若是已經配對過、但機器全被忘記的瀏覽器：`這個帳號還沒有任何機器回報。`
- **為什麼幫不了**：「那台機器」假設他已經有一台。實際上要做的是：安裝、`clawdline cloud login`、`clawdline cloud on`、重啟 daemon，
  想從手機下指令還要 `clawdline cloud commands on`。頁面上一個字都沒有。其實用 cookie 讀 `GET /v1/machines`，就分得出「0 台」和「沒配對」。
- **位置**：`next-strings.ts:208` → `CloudGate.tsx:838`；`next-strings.ts:215` → `CloudGate.tsx:931`；`web/console/src/legacy/devices-bridge.ts:21-23`
- **應該**：把「0 台」和「沒配對」分成兩個畫面。0 台時列出在電腦上要做的步驟和每一個指令。
- **紅燈**：假造一個 0 台機器的帳號，gate 的文字要含 `clawdline cloud login`。今天不含。

#### B11〔讀碼〕hosted：沒配對的那一列「尚未與這個瀏覽器配對」，整列按不下去（已知缺陷 1）｜U3
- **在哪**：「選一台機器」頁上沒配對的那一列；側欄「裝置」頁的卡片。
- **原字**：`尚未與這個瀏覽器配對`；裝置頁上是：`請在這台機器上選擇「Pair a Browser」，讓這個瀏覽器可以讀取它的 Sessions。`
- **為什麼幫不了**：這一列是 `disabled`，同一列上能按的只有 ✎ 和 ×。「Pair a Browser」這個按鈕在這個產品裡不存在：Mac 上叫「配對瀏覽器 → 產生配對 QR」，
  Linux 上是 `clawdline cloud pair`。
- **位置**：`zh-Hant.json:200` → `CloudGate.tsx:885-886`（`disabled` 在 `:876`）；`zh-Hant.json:202` → `web/console/src/legacy/js/view/devices.js:45`、`:137-140`
- **應該**：整列可以按，按下去打開「在這台機器上配對」的步驟，依平台給正確的按鈕名稱或指令。
- **紅燈**：`pairing:"not_paired"` 的那一列，要有一個沒有 `disabled`、而且不是 ✎/× 的按鈕。今天沒有。

#### B12〔讀碼〕hosted：觀看裝置滿了，畫面是一條死路｜U3
- **在哪**：登入時，帳號的觀看裝置已經到上限。
- **原字**：`帳號 {account} 的觀看裝置已經有 {limit} 台（{tier}）。這一版還不能在這裡移除舊裝置。`
- **為什麼幫不了**：畫面上沒有任何按鈕，也沒說可以去哪裡移除舊裝置。`tier` 缺值時會變成英文 `current plan`；
  `account` 在這個時間點還沒設（`setWho` 要等連上才呼叫），所以畫面上會出現「帳號  的」，中間是空的。
- **位置**：`next-strings.ts:209` → `CloudGate.tsx:841-846`、`:338`；`web/console/src/legacy/js/net/cloud-boot.js:289`
- **應該**：說出要去哪裡移除舊裝置，附上連結，至少給一顆「重新檢查」。
- **紅燈**：`data-cloud-screen="device_limit"` 的卡片裡，要至少有一個可以按的 `<button>` 或 `<a>`。今天一個都沒有。

#### B13〔讀碼〕hosted：機器沒開 Cloud 指令，送出之後才知道，說明也是錯的｜U3
- **在哪**：打開一個 session，打字，送出。
- **原字**：卡片上：`送不出去（cloud_commands_disabled）` 加 `再試一次`。輸入框下：`*現在不能送*——伺服器回報 \`write: false\`。這個框是接好的，那邊一改它就自己開。`
- **為什麼幫不了**：descriptor 不帶這個開關，所以手機事先不知道。「那邊一改它就自己開」是錯的：`write` 只會被設成 false，從不會變回 true。
  要打開只能在機器上跑 `clawdline cloud commands on`，但畫面沒說。
- **位置**：`zh-Hant.json:778` → `web/console/src/session/Composer.tsx:426`、`:612`；`write` 只被設成 false 的地方：`Composer.tsx:84`、`:407`、`:411`；
  `web/console/src/session/Transcript.tsx:294`、`:298`；`internal/transport/cloud/publish.go:266-273`；`cmd/clawdline/cloud.go:201-222`
- **應該**：直接說「這台機器目前只能看、不能下指令。在那台機器上執行 `clawdline cloud commands on`。」這個代碼不給「再試一次」。
  descriptor 公布這個開關，送成功一次就把輸入框打開。
- **紅燈**：relay 回 `cloud_commands_disabled` 之後，`#why` 要含 `clawdline cloud commands on`、不含 `write: false`，卡片上也不能有重試。今天三項都不成立。

#### B14〔讀碼〕Dashboard 的「派工」表單，在瀏覽器上一定被拒｜U2
- **在哪**：Dashboard →「派工」→ 展開，把欄位填完，按「派出去」。
- **原字**：邀請句是 `開一個新的 assistant session 來做一件事。`；送出之後：`TransportError: /v1/orchestrator/tasks answered 403 with no refusal in it`
- **為什麼幫不了**：瀏覽器永遠拿不到 orchestrator token，但使用者要先把所有欄位都填完（placeholder 還叫他填 `claims`），才會被擋。
  被擋之後看到的還是一行英文（成因見 M12）。
- **位置**：`web/console/src/Dashboard.tsx:711`、`:697`；`internal/transport/http/orchestrator.go:102-103`
- **應該**：瀏覽器裝置不畫這張表單，或事先說明「派工要從 session 裡發出」。
- **紅燈**：用 `--send` 開的分頁，Dashboard 上不能有可以按的「派出去」。今天有。

### 4.2 誤導（他會做錯事）

#### M01〔讀碼〕README 和 getting-started 對「沒有帳號的手機」說法相反，`clawdline tunnel` 也不會啟動 tunnel｜U1
- **在哪**：從 README 的「From a browser or a phone」讀到 getting-started §6。
- **原字**：README 寫 `**Works, over a tunnel you run.**`、`Start a tunnel with \`clawdline tunnel\``；getting-started 寫 `**There is no account-free way yet.**`。
  `clawdline tunnel` 實際只印出 `state     off`／`mode      off`／`binary    …`／`config    …/cloudflared.yml`。
- **為什麼幫不了**：兩份文件講的是相反的事。README 說的那個「啟動 tunnel 的指令」其實只會讀狀態，程式碼的註解也寫明 `It changes nothing`。
  mode 是 off 的時候，沒有任何一行說要怎麼打開。
- **位置**：`README.md:43`、`:202-207`；`docs/getting-started.md:157-161`；`cmd/clawdline/tunnel.go:16-19`、`:48-64`
- **應該**：兩份文件統一，寫出真正的步驟。`tunnel` 在 mode off 時印出怎麼打開。
- **紅燈**：用一個文件測試比對兩份文件對 account-free 的描述，要一致。今天不一致。

#### M02〔實測〕新使用者被指向作者本人的搬遷手冊｜U1
- **在哪**：getting-started §6 的「The steps for moving to it are in cloud-cutover.md」；`clawdline cloud preflight` 的最後一行；`cloud login` 失敗時的補救說明。
- **原字**：`next       the person's step: \`clawdline cloud login\`, then approve the code in the browser (docs/cloud-cutover.md step 2)`；
  還有 `the Swift app's ~/.config/clawdline is refused by construction`。
- **為什麼幫不了**：`docs/cloud-cutover.md` 是寫給作者一個人的遷移手冊，裡面有「全部由使用者本人做」、「步驟 0、3 是 root session 做」、「新版以『第二台 Mac』加入帳號」。
  新使用者沒有舊 app，也不知道誰是 root session。getting-started 裡還留著過期的句子，例如 macOS app 那段寫
  `starts that daemon with the three variables above`，但必要的變數現在只剩一個。
- **位置**：`internal/transport/cloud/preflight.go:143-147`；`internal/adapters/cloud/failure.go:110`；`docs/getting-started.md:145`、`:165-167`；`docs/cloud-cutover.md:1-12`
- **應該**：寫一份給新使用者的 Cloud 開通頁，CLI 的補救說明改指那一頁或直接寫出步驟；搬遷手冊只從內部文件連過去。
- **紅燈**：`grep -rn 'cloud-cutover' internal cmd` 在任何會印給使用者看的字串裡，要找不到。今天找得到 4 處。

#### M03〔實測〕新安裝拿到的派工規則，是作者那台機器的 house rules｜U1
- **在哪**：第一次 `serve`。log 寫著 `orchestrator: projected the shipped dispatch policy into …`；之後每一次派工，這份文件都會被貼進 child 的 briefing（2026-09-25 起不再貼：briefing 只帶使用者自己的 local 檔，base 改成指出檔案路徑，見 `internal/app/orchestrator/policy.go` 的 `childPolicy`）。
- **原字**：`# How work is handed out on this machine`，後面寫著 `**Clawdfather** is the exception…`、`Measured on one line here: the implementation cost $30.90…`、
  `Until a focused Swift runner ships…`、`No count is copied back into \`test.sh\`…`
- **為什麼幫不了**：這份文件被 `go:embed` 進 binary，當成產品的預設值，而且是「這台機器」的口吻。新使用者派出去的每一個 child，
  都會讀到一台它不在的機器上量出來的數字，以及它的 repo 裡不存在的 `test.sh`、Swift runner、Clawdfather。
- **位置**：`internal/app/orchestrator/dispatch-policy.md:1`、`:42`、`:180`、`:202`、`:211`；`internal/app/orchestrator/policy_projection.go:43-49`；
  `internal/transport/http/orchestrator_wiring.go:271-275`
- **應該**：產品只附一份通用、短的預設規則。作者自己的規則放進他本機的 `dispatch-policy.local.md`。
- **紅燈**：新安裝投射出來的 `dispatch-policy.md` 裡，不能有 `Clawdfather`、`Swift`、`test.sh`、金額。今天四個都有。

#### M04〔讀碼〕`clawdline open` 在沒有 console 時照樣報成功，也不說這個分頁只能看｜U2
- **在哪**：getting-started §3，daemon 啟動時忘了設 `CLAWDLINE_NEXT_WEB`，然後跑 `clawdline open`。
- **原字**：終端機顯示 `opened a browser as device <一串 36 字元的 UUID>`；
  瀏覽器顯示 `{"error":"no_web_root","detail":"this daemon was not told where the console is; start it with CLAWDLINE_NEXT_WEB set to the console's built files"}`
- **為什麼幫不了**：終端機說成功、exit 0，瀏覽器看到的卻是一段英文 JSON。印出來的是 UUID，也沒說這個裝置只能讀。
- **位置**：`cmd/clawdline/auth.go:111-141`；`internal/transport/http/page.go:76-79`；`internal/transport/http/auth.go:474`
- **應該**：打開瀏覽器之前先讀 `console.state`，不是 `served` 就 exit 1，並說出修法。成功時印：
  `Signed in "Browser on this machine" (read only). To type into sessions too: clawdline open --send`。
- **紅燈**：daemon 沒設 WEB 時，`clawdline open; echo $?` 要是非 0。今天是 0。

#### M05〔讀碼〕已失效的 `clawdline open` 連結，會安靜地落到配對門｜U2
- **在哪**：打開一個裝置已被撤銷、或 cookie 換不到的 `clawdline open` 網址。
- **原字**：`這個瀏覽器還沒被放進來。`，下面的說明行是空的。
- **為什麼幫不了**：換 cookie 的結果是 `.then(home, home)`，不管成功還是被拒都回首頁，拒絕的原因被吞掉了。他會以為要配對，其實該做的是重跑 `clawdline open`。
- **位置**：`internal/transport/http/auth.go:475`、`:481`；`web/console/src/door/Door.tsx:201`
- **應該**：失敗時帶著原因回首頁，門口說「這個連結已經不能用了，在這台機器上重新執行 `clawdline open`」。
- **紅燈**：用已撤銷的 token 開 `/v1/auth/open#t=…`，`#door-say` 要有句子。今天是空的。

#### M06〔讀碼〕配對碼過期，門口卻說「問不到」｜U2
- **在哪**：在門口打六位數字，但這組配對已經被另一次「要求配對」取代，或已經過期。
- **原字**：`問不到那台 Mac。（expired）`
- **為什麼幫不了**：明明問到了，只是這組碼過期了。他會去查網路或 daemon。`store_unavailable`、`device_list_full` 也會掉進同一句。
- **位置**：`zh-Hant.json:210`；`web/console/src/door/Door.tsx:404-409`
- **應該**：`expired` 用 `webDoorExpired` 那句，並回到第一步；`device_list_full` 說「裝置名單滿了」。
- **紅燈**：confirm 回 403 `expired` 時，門口不能出現 `問不到`。今天會出現。

#### M07〔讀碼〕被鎖 24 小時，門口卻說「十分鐘三次」｜U2
- **在哪**：24 小時內打錯五次，之後再按「要求配對」。
- **原字**：`這次配對結束了——錯了五次。等你人在 Mac 旁邊再問一次。`，接著是 `每問一次，那台 Mac 上就跳一次提示，所以十分鐘只有三次。（rate_limited）`
- **為什麼幫不了**：真正的原因是 24 小時的鎖定，但 daemon 把鎖定和限流都叫 `rate_limited`。他會每十分鐘回來試一次，每次都被拒。
- **位置**：`internal/transport/http/auth.go:118-126`；`internal/domain/auth/authority.go:33`、`:349-351`；`web/console/src/door/Door.tsx:363-368`、`:398`；`zh-Hant.json:220`、`:229`
- **應該**：鎖定用獨立的代碼，帶上解鎖時間，並提示人在這台機器上可以直接 `clawdline open`。
- **紅燈**：錯五次之後，`POST /v1/auth/pair` 回的代碼不能是 `rate_limited`。今天是。

#### M08〔讀碼〕本機被拒，畫面卻說「Cloud 不允許」｜U2
- **在哪**：唯讀分頁按 ＋ 選一個專案，或用麥克風錄一段。
- **原字**：`Cloud 不允許這台裝置這麼做。（forbidden）`
- **為什麼幫不了**：本機 console 沒有經過 Cloud，他會跑去找 Cloud 的設定。錯誤代碼表只有 Cloud 的措辭。
- **位置**：`web/console/src/legacy/js/core/failure-text.js:25`、`:27`；`zh-Hant.json:248`、`:264`；`web/console/src/session/Start.tsx:146`、`:544`
- **應該**：本機傳輸用本機的句子：「這個分頁沒有打字權限，用 `clawdline open --send` 重開。」
- **紅燈**：本機 console 上任何 `forbidden` 的畫面文字，都不能含 `Cloud`。今天含。

#### M09〔讀碼〕專案詳情說「沒有任何 worktree 完成過 Feature」，其實是 daemon 量不到｜U4-A
- **在哪**：側欄「專案」→ 點任一列。
- **原字**：`這個專案底下沒有任何 worktree 完成過 Feature。`
- **為什麼幫不了**：Go daemon 沒有記錄 Feature 歸屬，回應裡寫死 `"featureRows": 0`、`"worktrees": []`。「量不到」被畫成「沒有」。
- **位置**：`internal/adapters/analytics/worktrees.go:135-136`；`zh-Hant.json:573`；`web/console/src/legacy/js/view/projects.js:379`、`:382`
- **應該**：回 `unknown` 並附理由，畫面說「這台機器不記錄 Feature 歸屬」。
- **紅燈**：任何一個有使用紀錄的專案，詳情頁都還會出現這句。今天一定會出現。

#### M10〔實測〕第一個 session 的對話區是一段英文，狀態列永遠「載入中⋯」｜U4-A
- **在哪**：打開一個剛啟動、還沒送出第一則訊息的 session。在我的實測裡，是停在登入畫面的 Codex。
- **原字**：`this session's own record could not be located: no transcript is open yet: Codex writes a thread's rollout at its first message, not at startup`；狀態列是 `codex` `載入中⋯`
- **為什麼幫不了**：daemon 的內部 note 被原封不動顯示出來。狀態列拿不到 info 就一直畫「載入中」，不會結束。
  這時候他真正需要知道的是 Codex 在等他登入，但終端機畫面不會出現在這裡（見 M11）。
- **位置**：`internal/transport/http/usage.go:119-121`；`web/console/src/session/Transcript.tsx:177`；`web/console/src/session/StatusLine.tsx:90`；`zh-Hant.json:441`
- **應該**：依原因代碼選中文句子，例如「這個 session 還沒有對話紀錄，送出第一則訊息之後就會出現」。沒有紀錄時，改顯示即時畫面。狀態列不要無限期載入。
- **紅燈**：transcript 回 `evidence:"none"` 並帶英文 note 時，`.tx-note` 不能含英文句子。今天會原樣顯示。

#### M11〔實測〕第一個 session 疊著三句死句子，而畫面其實讀得到｜U4-B
- **在哪**：清單上剛出現的第一個 session。實測是停在登入畫面的 Codex，在 tmux 裡，cwd 是 `my-app`。
- **原字**：列上是 `畫面讀不到`／`狀態未知`／`無法判斷能否關閉`；詳細頁副標題是 `…/my-app · ttys037 · 畫面讀不到`（路徑前段省略）
- **為什麼幫不了**：我直接打 `GET /v1/sessions/%250/screen`，回的是 `readable:true`，登入選單的原文都讀得到。
  daemon 在 row 上其實有帶 `activity.unknown_reason: "no_record"` 和一句 detail，但 console 只看 `state === "unknown"`，一律畫成「畫面讀不到」。
  `unknown_reason` 在整個 console 只出現在一行註解裡。這三句加起來，沒有一句告訴他該做的事：去終端機完成 Codex 登入。
- **位置**：`web/console/src/session/List.tsx:125-126`；`web/console/src/session/Detail.tsx:271`；`zh-Hant.json:744`、`:38`、`:16`；`web/console/src/session/order.ts:53`（唯一提到 `unknown_reason` 的地方）
- **應該**：依 `unknown_reason` 說話，例如 `no_record` 就說「還沒有對話紀錄」，並顯示即時畫面。「畫面讀不到」只留給畫面真的讀不到的時候。
- **紅燈**：row 帶 `unknown_reason:"no_record"`、而且畫面讀得到時，列上不能出現 `畫面讀不到`。今天會出現。

#### M12〔讀碼〕daemon 回的巢狀拒絕，client 認不得，變成 `unexpected_error` 或英文｜U4-B
- **在哪**：唯讀分頁 → session 的 ⋯ →「在 Mac 上顯示」或「關閉 session」；⌘I 資訊卡切模型；Dashboard 派工或送出；「現在」頁在 daemon 不通時。
- **原字**：`請求失敗（unexpected_error）`；`讀不到這個 session 的資訊。（unexpected_error）`；`TransportError: /v1/orchestrator/tasks answered 403 with no refusal in it`
- **為什麼幫不了**：gate、actions、focus、dispatch 回的格式是 `{"error":{"code":…}}`，但 core client 的 `isRefusal` 只認平的 `{error, detail}`。
  一個明確的 `forbidden` 就這樣變成沒有代碼的 `TransportError`。`cloud/relay-writer.ts:400-406` 的註解早就記下這件事。
- **位置**：`web/core/src/refusal.ts:46-53`；`web/core/src/client.ts:177-178`；`internal/transport/http/gate.go:732-743`；`web/console/src/overlays/action-confirm.ts:424`、`:439`；
  `web/console/src/overlays/info.ts:101-104`；`web/console/src/Dashboard.tsx:583`、`:697`；`web/console/src/cloud/relay-writer.ts:400-406`
- **應該**：core client 平的和巢狀的都讀，做法照 `web/console/src/session/sender.ts:88-102` 的 `refusalOf`；或 daemon 統一成一種格式。
- **紅燈**：唯讀裝置送 `/v1/sessions/<id>/close` 之後，toast 要含 `forbidden`、不含 `unexpected_error`。今天正好相反。

#### M13〔讀碼〕還沒讀到清單時，頁首說「沒有 session」；清單說「在等 app」｜U4-B
- **在哪**：第一份清單回來之前；或關掉跑 `serve` 的終端機之後。
- **原字**：頁首 `沒有 session` 加 `離線`；清單 `在等 app`／`串流上還沒有東西進來`
- **為什麼幫不了**：沒有 snapshot 時 rows 是空的，計數就畫成 0。「在等 app」沒說 app 是哪一個，也沒說要重開 daemon。
- **位置**：`web/console/src/App.tsx:355`、`:734`、`:893-895`；`zh-Hant.json:193`、`:242-243`；`web/console/src/Sessions.tsx:151-152`
- **應該**：沒有 snapshot 時計數畫 `—`；清單說「連不到這台機器上的 Clawdline。執行 `clawdline serve`（或重新打開 app），這裡會自己接上。」
- **紅燈**：第一次 `/v1/sessions` 失敗時，`#counts` 不能是 `沒有 session`。今天是。

#### M14〔讀碼〕工作看板讀不到時，四區照畫 `0` 和「目前沒有東西」｜U4-B
- **在哪**：側欄「看板」，daemon 不通或 `/v1/work/board` 被拒。
- **原字**：`讀不到看板。`，下面四區各是 `0` 和 `這一區目前沒有東西。`
- **為什麼幫不了**：上面剛說讀不到，下面又說確定是 0。
- **位置**：`web/console/src/pages/work/Board.tsx:82`、`:92-93`；`web/console/src/pages/work.tsx:44-47`
- **應該**：讀不到時計數畫 `—`，也不畫空狀態的句子。
- **紅燈**：回 503 時，還是有 `.work-count` 顯示 `0`。

#### M15〔讀碼〕Dashboard 在讀不到時寫 `Sessions 0`｜U4-B
- **在哪**：Dashboard，daemon 不通。
- **原字**：`Sessions` `0`，下一行是 `這次掃描不完整，所以空白不代表沒有東西在跑。`
- **位置**：`web/console/src/Dashboard.tsx:29`、`:336`、`:339`
- **為什麼幫不了**：數字和它下一行的說明互相矛盾。
- **應該**：沒有 snapshot 就畫 `—`。
- **紅燈**：第一次讀取失敗時，計數是 `0`。

#### M16〔讀碼〕驗證帳本還在掃描時說「讀不到」，而且不會重試｜U4-B
- **在哪**：第一次打開「驗證帳本」，使用紀錄還在掃描中。
- **原字**：`讀不到驗證帳本。（usage_analytics_busy）`
- **為什麼幫不了**：這個狀態只要等一下就好，畫面卻畫成故障。沒有重試按鈕，也不會自己重讀。
- **位置**：`internal/transport/http/analytics.go:22`、`:102`；`web/console/src/legacy/js/view/ledger.js:386-387`；`zh-Hant.json:395`
- **應該**：說「還在讀使用紀錄，幾秒後會自己再試」，並照 `Retry-After` 自動重讀。
- **紅燈**：回 503 `usage_analytics_busy` 後，5 秒內沒有自動重送。

#### M17〔讀碼〕排程的專案清單讀不到，一律說「連不到 Clawdline」｜U4-B
- **在哪**：排程 ＋ →「專案」。
- **原字**：`連不到 Clawdline。它還在那台 Mac 上跑著嗎？`；清單是空的時候按建立：`不確定是哪個專案，自己選一個。`
- **為什麼幫不了**：任何錯誤都被說成 daemon 不在。清單是空的，他根本沒東西可選。
- **位置**：`web/console/src/pages/schedules.tsx:559`、`:650`、`:383`；`zh-Hant.json:488`、`:633`
- **應該**：讀取失敗走 `failureSentence`；清單是空的時候，把「建立」反灰，並說明目錄要怎麼出現。
- **紅燈**：`/v1/places` 回 500 時，還是顯示 `webOffline` 那句。

#### M18〔讀碼〕裝置頁在讀不到時照樣寫「在線」｜U4-B
- **在哪**：側欄「裝置」。
- **原字**：`在線`、`已與這個瀏覽器配對`
- **為什麼幫不了**：本機那張卡片的 `freshness: "current"` 和 `pairing: "local"` 是寫死的，daemon 停了也畫在線；也從不說這個分頁只能看。
- **位置**：`web/console/src/legacy/devices-bridge.ts:190-191`；`zh-Hant.json:201`、`:203`
- **應該**：實際讀一次 health；唯讀時寫出 `clawdline open --send`。
- **紅燈**：daemon 停掉之後進「裝置」，仍然顯示「在線」。

#### M19〔讀碼〕hosted：「0 個 session」和「離線或狀態資料已過期」，其實是不知道（已知缺陷 2）｜U4-C
- **在哪**：「選一台機器」上每一列的第二行。
- **原字**：`0 個 session`、`離線或狀態資料已過期`
- **為什麼幫不了**：`sessions` 算的是「這個瀏覽器已經解開的 snapshot 有幾筆」。沒配對的機器永遠是 0；已配對但清單還沒到的，也先畫 0。
  程式裡明明有 `sessionInventoryByMachine` 可以分辨「確定是 0」和「還不知道」，卻沒用。
  `freshness:"unknown"`（從沒看過回報）也被畫成「離線或已過期」，而裝置頁對同一個狀態寫的是「狀態未知」。
- **位置**：`next-strings.ts:218` → `CloudGate.tsx:881`（數字來自 `web/console/src/legacy/js/net/cloud-client.js:2013-2016`）；`zh-Hant.json:735` → `CloudGate.tsx:887-889`
- **應該**：不知道數量就不寫數量；`unknown` 用「狀態未知」。
- **紅燈**：`{pairing:"not_paired",sessions:0}` 的那一列不能含 `0 個 session`；`freshness:"unknown"` 的那一列不能含 `離線或狀態資料已過期`。

#### M20〔讀碼〕hosted：手機自己沒網路，卻怪 Cloud 沒回應｜U4-C
- **在哪**：第一次打開時網路斷了。
- **原字**：`Clawdline Cloud 沒有回應（offline），3 秒後再試。`；重試用完之後：`連不到 Clawdline Cloud（retries_exhausted）。`
- **為什麼幫不了**：`fetch` 丟出的 TypeError 沒有代碼，gate 就補上 `offline`，再說成是 Cloud 沒回應。秒數只在開始時算一次，不會倒數；也沒有「立刻重試」。
- **位置**：`CloudGate.tsx:389`、`:394`；`next-strings.ts:203` → `:848`；`next-strings.ts:204` → `:852`
- **應該**：`navigator.onLine === false` 時說「這支手機現在沒有網路」；秒數要倒數；給一顆「立刻重試」。
- **紅燈**：`navigator.onLine=false` 時，重試畫面不能含 `Clawdline Cloud 沒有回應`。

#### M21〔讀碼〕送出失敗的卡片一律寫「送不出去（代碼）」，而且都給「再試一次」｜U4-C
- **在哪**：任何被拒的送出：機器離線、唯讀、這台機器不認得這個瀏覽器；本機和 hosted 都一樣。
- **原字**：`送不出去（machine_offline）` 加 `再試一次`
- **為什麼幫不了**：`Composer.tsx:412-416` 說這個問題已經修掉，但修的只是 catch 那一條路。卡片仍然手動組 `sendFailed`，
  所以 `failure-text.js` 為每個代碼寫好的句子都被跳過。重試對「唯讀」這類代碼永遠不會成功。
- **位置**：`zh-Hant.json:26`、`:272` → `web/console/src/session/Transcript.tsx:294`；對照 `web/console/src/legacy/js/core/failure-text.js:19-81`
- **應該**：卡片也用 `failureSentence`；會讓輸入框關掉的代碼不給重試。
- **紅燈**：`failed(token,"machine_offline")` 之後，卡片文字不能是 `送不出去（machine_offline）`。

#### M22〔讀碼〕hosted：選的那台機器已經離線，頁首還寫「已連上」｜U4-C
- **在哪**：在機器清單選了一台已經沒在回報的機器，進入 console。
- **原字**：`已連上`；清單是空的時候：`在等這台機器的清單`／`線已經通了。這台機器還沒說它現在有哪些 session。`
- **為什麼幫不了**：頁首的燈號量的是 relay 連線，不是那台機器。整個 console 沒有一處說「這台機器可能沒開」，也沒說可以回去換一台。
- **位置**：`web/console/src/cloud/relay-reader.ts:577-584`；`zh-Hant.json:107` → `web/console/src/App.tsx:922-924`；`next-strings.ts:216-217` → `Sessions.tsx:155`
- **應該**：燈號或橫幅反映 `machine.freshness`，例如「這台機器 N 分鐘前最後回報」；等太久時提供「換一台機器」。
- **紅燈**：選一台 `freshness:"stale"` 的機器之後，`#conn[data-state]` 不能是 `live`。

#### M23〔讀碼〕hosted：「有一台機器的資料在這裡讀不出來」，沒說是哪一台｜U4-C
- **在哪**：機器清單的最下面。
- **原字**：`有一台機器的資料在這裡讀不出來（{code}）。`
- **為什麼幫不了**：沒說是哪一台，也沒說該做什麼，只給一個錯誤代碼，例如 `unknown_sender`。
- **位置**：`next-strings.ts:219` → `CloudGate.tsx:943`（`ACCESS_PROBLEMS` 在 `:102-106`）
- **應該**：把句子放到那台機器的那一列上，並依代碼給動作，例如「在那台機器上重新配對這個瀏覽器」。
- **紅燈**：`problem="unknown_sender"` 時，`.say` 要含那台機器的名字。

#### M24〔實測〕第一個 session 的名字是 `Codex · ttys037`，但 daemon 早就知道它在 `my-app`｜U5
- **在哪**：清單上的第一個 session，以及它的詳細頁標題。
- **原字**：`Codex · ttys037`
- **為什麼幫不了**：名字的最後一階 `session.Coordinate` 是「助手 · tty」，沒有 tty 時用 pane id（`%0`）。沒有任何一階用到 cwd。
  新使用者每開一個 session，在送出第一則訊息之前，看到的都是一個終端機裝置名稱。開了兩個以上，就分不出誰是誰。
- **位置**：`internal/domain/session/identity.go:62-82`；`internal/domain/session/label.go:34-41`；`internal/app/inventory.go:244-245`
- **應該**：在 tty 前面加一階「助手 · 專案目錄名」（`Codex · my-app`）。tty 放進副標題。
- **紅燈**：cwd 是 `…/my-app`、還沒有任何紀錄的 session，`label` 要含 `my-app`。今天是 `Codex · ttys037`。

#### M25〔讀碼〕hosted：「機器 · a1b2c3d4」是合成的名字，連改名欄都預填它（已知缺陷 3）｜U5
- **在哪**：機器清單的列名、頁首右上的機器 pill、改名欄的預設值、忘記和改名的對話框、裝置卡片。
- **原字**：`機器 · a1b2c3d4`（`webStartMachine` 加上 id 的**最後** 8 碼；原始描述說是前 8 碼，其實是 `slice(-8)`）
- **為什麼幫不了**：`machinePresentation` 沒有 descriptor 時自己合成一個名字，放在 `name` 欄位回傳，所以每個呼叫的地方都以為「有名字」。
  改名欄預填這串，使用者很可能直接按儲存，把 id 片段存成正式名字。頁首 pill 是選擇當下存起來的快照，就算 descriptor 後來到了，也要重新載入才會更新。
- **位置**：`zh-Hant.json:732`；`web/console/src/legacy/js/session/selection.js:43-46`、`:62-65`；`CloudGate.tsx:879`、`:470`、`:554`、`:597`、`:666`；
  `web/console/src/legacy/js/view/devices.js:29-31`、`:39`、`:120-122`
- **應該**：沒有 descriptor 時，用帳號 `GET /v1/machines` 上的名字。真的沒有名字，就寫「一台還沒命名的機器」，id 只放進可以展開的細節。改名欄不預填合成的名字。
- **紅燈**：一台 id 結尾是 `a1b2c3d4`、沒有 descriptor 的機器，`.cloud-machine-name` 和 `#cloud-rename-name` 的值都不能含 `a1b2c3d4`。

#### M26〔實測〕每一台機器都叫「Mac」，而守衛刻意不讀畫面上的字｜U6-a
- **在哪**：配對門（B04）、方案頁、送出的狀態、失敗句、語音。在 Linux 上也一樣。
- **原字**：`看那台 Mac。`、`這個視窗連的是你面前這台 Mac，不是 Clawdline Cloud。`、`Mac 已收到 · 正在同步對話紀錄⋯`、`Mac 目前不在線上。`、`這個帳號還沒有 Mac 連上。`、`Mac 上沒有裝 Whisper…`
- **為什麼幫不了**：這個 daemon 也跑在 Linux 上。`zh-Hant.json` 一共 778 個 key，有 102 行寫著「Mac」。它是舊 Swift app 的逐位元組複本
  （`web/console/src/legacy/MANIFEST.json:94`），`check-machine-words.sh` 也明寫不讀它，理由是「改它是 drift，不是修正」。
  所以守衛是綠的（`machine words: 721 files clean`），畫面上的字卻沒人負責。U2、U3、U4 裡每一句要改的字，都會撞到同一堵牆。
- **位置**：`tools/check-machine-words.sh:17-22`；`web/console/src/legacy/MANIFEST.json:94`；`zh-Hant.json:216`、`:500`、`:593`、`:251`、`:256`、`:764`
- **應該**：看 §5 D2 的決定。不管選哪一條，守衛都要掃畫面實際吃到的字。
- **紅燈**：`platform:"linux"` 的機器，配對門和送出卡片不能含 `Mac`。今天含。

#### M27〔讀碼〕舊看板把「永遠不會有」畫成「稍後重試」，而且每 15 秒重試一次｜U6-b
- **在哪**：工作 → 項目 → 時間軸 →「看板」。
- **原字**：`專案紀錄暫時無法取得，稍後會在背景重試。`
- **為什麼幫不了**：沒有 Swift 看板檔時，每個專案都回 `project_not_found`。這是永久的狀態，但設定預設 `Enabled: true`，所以會一直輪詢下去。
- **位置**：`internal/adapters/board/envelope.go:201-205`；`internal/adapters/board/settings.go:62`；`web/console/src/legacy/js/view/board.js:1913`
- **應該**：回一個獨立的代碼，頁面改指向「工作」頁，並停止輪詢。
- **紅燈**：30 秒內送出不只一次 `/v1/board?project=`。

#### M28〔讀碼〕驗證帳本叫人等一個不存在的「回填」｜U6-b
- **在哪**：側欄「驗證帳本」。前提是過去用過 claude，但還沒派過工。
- **原字**：`這台 Mac 上有 {rows} 列沒有帶 Feature 鍵，所以下面每一個數字都算不到它們。回填是在 app 啟動時跑的。`
- **為什麼幫不了**：一般的 claude 使用紀錄全被當成缺陷，而 daemon 裡根本沒有回填這件事。
- **位置**：`internal/transport/http/ledger.go:222`、`:226`；`zh-Hant.json:418-419`；`web/console/src/legacy/js/view/ledger.js:300`、`:303`
- **應該**：一般對話不列為缺陷，拿掉回填那句。
- **紅燈**：store 裡只有一般紀錄時，畫面仍然出現 `回填是在 app 啟動時跑的`。

#### M29〔讀碼〕時間軸承諾 production 部署證據，但這個 daemon 不產生｜U6-b
- **在哪**：工作 → 項目 → 時間軸。
- **原字**：`以來源憑證核對 production 是否真正可用`
- **位置**：`internal/transport/http/timeline.go:29`；`web/console/src/legacy/js/view/timeline.js:129`、`:185`
- **為什麼幫不了**：承諾的東西永遠不會出現，看起來像壞掉了。
- **應該**：改寫成「已合併進 Git 的交付」，拿掉環境篩選。
- **紅燈**：副標題還含 `production`。

#### M30〔讀碼〕設定頁的「啟用專案時間軸」永遠停在英文 `Loading…`｜U6-b
- **在哪**：側欄「設定」。
- **原字**：`Loading…`（按鈕是 disabled）
- **為什麼幫不了**：這個開關在這個 daemon 上不存在，bridge 也沒收這個 id，所以永遠不會更新。
- **位置**：`web/console/src/pages/settings.tsx:306-307`；`web/console/src/legacy/timeline-bridge.ts:11-13`、`:31-48`
- **應該**：拿掉這顆按鈕，改一句「時間軸一直開著」。
- **紅燈**：等 5 秒之後，文字仍然是 `Loading…`。

#### M31〔讀碼〕設定頁看板的兩個開關永遠是灰的，沒說為什麼｜U6-b
- **在哪**：「設定」→「啟用看板系統」、「AI 閱讀摘要」。
- **原字**：兩顆按鈕都是 disabled，沒有任何說明；讀取失敗時整塊變成英文，例如 `Enable Project Board`。
- **為什麼幫不了**：要有 `admin` 才能改，只有本機 token 有，瀏覽器永遠按不下去。Linux 上也沒有別的地方能改。
- **位置**：`web/console/src/pages/settings/BoardBlock.tsx:136-193`、`:150`、`:186`
- **應該**：寫明誰能改、在哪裡改；fallback 用中文。
- **紅燈**：`--send` 分頁的設定頁上，沒有任何「為什麼不能改」的說明。

#### M32〔讀碼〕「看板」這個詞指兩個不相干的東西｜U6-b
- **在哪**：側欄的「看板」，對照「設定 → 啟用看板系統」。
- **原字**：`看板`、`啟用看板系統`
- **為什麼幫不了**：側欄讀的是 `/v1/work/*`，設定的開關管的是 `/v1/board`。打開設定裡的開關，側欄的看板一點變化都沒有。
- **位置**：`web/console/src/pages/work/words.ts:148`（渲染在 `App.tsx:802`）；`BoardBlock.tsx:136`
- **應該**：分成「工作看板」和「專案看板」兩個名字。
- **紅燈**：兩處的名字還是一樣。

#### M33〔讀碼〕派工關著時，提示指向一個不存在的「設定 → 遠端」｜U6-b
- **在哪**：`orchestrator_enabled=false` 時建立排程。
- **原字**：`排程建好了，也有效，只是派工是關著的，不會有人執行它。在 Mac 上的「設定 → 遠端」可以打開。`
- **為什麼幫不了**：開關其實在原生設定視窗的「派工作給別的 session」分頁；Linux 上沒有任何地方能打開。
- **位置**：`zh-Hant.json:623` → `web/console/src/pages/schedules.tsx:897`
- **應該**：指向正確的地方；沒有原生視窗的平台，給另一條路。
- **紅燈**：字串裡沒有「派工作給別的 session」。

#### M34〔讀碼〕空清單的提示是錯的，也不說沒有 tmux｜U7
- **在哪**：首頁清單，0 個 session。
- **原字**：`找不到在跑 Claude Code 的分頁`／`在終端機裡開一個，它就會出現在這裡`
- **為什麼幫不了**：文件自己說只有 tmux 和 iTerm2 裡的 session 會列出來。daemon 知道沒裝 tmux（capabilities 裡有），但這件事沒送到畫面上。
  句子沒提 Codex，也沒提 ＋。只用 Terminal.app 的人會不會出現在清單裡，我在這台機器上走不到（§2）。
- **位置**：`zh-Hant.json:24`、`:241`；`web/console/src/Sessions.tsx:154`、`:263-270`；`internal/adapters/terminal/capabilities.go:64`
- **應該**：「還沒有 session。在 tmux（Mac 上也可以用 iTerm2）裡跑 `claude` 或 `codex`，或按上面的 ＋ 開一個。」沒有 tmux 時直接說出來。
- **紅燈**：沒裝 tmux 的機器清單是空的時候，畫面要含 `tmux`。今天不含。

#### M35〔實測〕`doctor` 什麼都不檢查，連 port 都印錯｜U7
- **在哪**：getting-started §2 的 Check；或找不到 session、沒裝 tmux、沒裝助手的時候。
- **原字**：我的 daemon 跑在 7791，`CLAWDLINE_NEXT_PORT=7791 clawdline doctor` 印出來的是：
  `version   0.0.1-p0`／`port      7727`／`upstream  none (an unowned route answers 501 not_implemented; CLAWDLINE_NEXT_UPSTREAM_PORT asks for one)`／`dir       …`／`store     0 events, 0 broker tasks`
- **為什麼幫不了**：
  - `doctor` 讀的是 `config.Load().Port`，它永遠是 7727；其他子指令用的是 `daemonPort()`。
  - daemon 沒在跑、沒裝 tmux、沒裝 claude、console 是 NONE，這幾種情況的輸出一模一樣，而且都 exit 0。
  - `upstream` 是搬遷時期的用語。
  - daemon 其實早就知道答案（`/v1/diagnostics`、`installedAssistants`）。
- **位置**：`cmd/clawdline/main.go:221-247`；`internal/config/config.go:66-67`；`cmd/clawdline/auth.go:33-41`；`README.md:180`
- **應該**：逐項印 ok 或「需要動作」，並附上要跑的指令：daemon、console、tmux、claude/codex、Cloud。只要有一項是紅的，就 exit 非 0。
- **紅燈**：daemon 沒在跑、`PATH` 上也沒有 tmux 時，`clawdline doctor` 要 exit 非 0，並說出這兩件事；`CLAWDLINE_NEXT_PORT=7800` 時要印 7800。今天三項都不成立。

#### M36〔讀碼〕`cloud off` 不會切斷正在跑的 daemon，CLI 也不說｜U8（涉及安全）
- **在哪**：getting-started §6 的 `cloud on`，以及之後想關掉時的 `cloud off`。
- **原字**：`cloud off  ~/.config/clawdline-next/config.json`
- **為什麼幫不了**：daemon 只在啟動時讀一次開關，所以 `off` 之後那條線還連著，已配對的 viewer 照樣讀得到。
  `cloud status` 讀的是設定檔，會顯示 `enabled false`，跟 daemon 實際的狀態對不上。文件只在 `on` 那裡提醒要重啟，`off` 那裡沒提。
- **位置**：`cmd/clawdline/cloud.go:191-195`；`internal/transport/cloud/link.go:256`、`:438`；`docs/getting-started.md:176-178`
- **應該**：`off` 讓 daemon 當場斷線。做不到的話，就去問 daemon 的狀態，不一致時說出要重啟。
- **紅燈**：線還連著時跑 `cloud off`，接下來 `GET /v1/cloud/status` 的 `state` 要是 `off`，或 CLI 輸出含 `restart`。今天兩者都不成立。

#### M37〔讀碼〕`cloud on` 之後沒重啟就 `cloud pair`，被叫去重新 login，而重新 login 會直接換掉身分｜U8
- **在哪**：§6 的 login → on →（沒有重啟）→ `cloud pair`。
- **原字**：`clawdline: This machine is not connected to a Clawdline Cloud account. Run \`clawdline cloud login\` first. (cloud_not_signed_in)`；
  照做之後出現 `replacing  machine 7d1e…-c0a3 registered with https://api.clawdline.com`，然後直接繼續。
- **為什麼幫不了**：真正的原因是還沒重啟，他卻被導去重新註冊，而重新註冊不會先問一聲。
- **位置**：`internal/transport/http/cloud.go:284-285`、`:293-297`；`cmd/clawdline/main.go:203-213`；`internal/transport/cloud/link.go:256-258`；`cmd/clawdline/cloud.go:257-261`
- **應該**：分辨出「設定是開的、但 daemon 啟動時是關的」這個狀態，並說要重啟。已經有身分時，`cloud login` 要先確認，或要求加 `--replace`。
- **紅燈**：照上面的順序跑 `cloud pair`，stderr 要含 `restart`、不含 `cloud login`。今天正好相反。

#### M38〔讀碼〕`cloud devices` 讀不到清單時，說成「從沒配對過」｜U8
- **在哪**：§6 的 `cloud devices`，在線路沒開、或身分讀不到的時候。
- **原字**：`no browser has been paired with this machine`／`run \`clawdline cloud pair\` to show one a link`（我在全新狀態實際跑出這兩行，那一次是真的沒有配對）
- **為什麼幫不了**：線路沒開時，`pinned` 和 `roster` 都是 nil，檢查就過了，「讀不到」被畫成 0。已經配對過的人會以為清單被清空，於是又去配對一次。
- **位置**：`cmd/clawdline/cloudpair.go:138-145`；`internal/transport/cloud/link.go:759-773`
- **應該**：線路沒開或沒設定時，說「這個 daemon 的 Cloud 線路沒開，所以讀不到已配對的清單」。
- **紅燈**：配對過一個瀏覽器，再 `cloud off` 並重啟，`cloud devices` 不能出現 `no browser has been paired`。

#### M39〔讀碼〕文件說 `cloud status` 會回報「connected」並「給出原因」，其實它只印設定檔｜U8
- **在哪**：§6 的 Check，以及 troubleshooting 表的「The Cloud line stays off」。
- **原字**：文件寫 `**Check:** \`cloud status\` reports the line as connected`、`\`./bin/clawdline cloud status\` gives the reason`；
  實際輸出只有 `enabled`、`commands`、`relay`、`api`、`keys`、`identity`、`device key`（我在全新狀態跑過）。
- **為什麼幫不了**：CLI 不去問 daemon，所以沒有狀態、沒有最後一次錯誤、也沒有該怎麼做。文件寫的 Check 永遠過不了。
- **位置**：`docs/getting-started.md:203`、`:217`；`cmd/clawdline/cloud.go:138-179`
- **應該**：另外讀 daemon 的 `/v1/cloud/status`，印出 `line connected since …`，或 `line down: <kind>` 加上該怎麼做。
- **紅燈**：relay 拒絕連線時跑 `cloud status`，輸出要含 `what to do`。

#### M40〔讀碼〕`cloud commands on` 一律宣稱「viewer 可以打字了」｜U8
- **在哪**：§6 的 `cloud commands on`。
- **原字**：`a paired viewer may now type into this machine's sessions`（我在沒有任何身分、沒有任何 viewer 的全新狀態，實際跑出這一行）
- **為什麼幫不了**：能不能打字，還要看每個裝置的權限裡有沒有 `send_prompt` 或 `start_session`，而且 daemon 的線路要是開的。CLI 兩件都沒檢查。
- **位置**：`cmd/clawdline/cloud.go:216-222`；`internal/transport/cloud/link.go:678`
- **應該**：列出每個 viewer 實際的結果，並附上線路的狀態。
- **紅燈**：一個 viewer 都沒有時，輸出不能說 `may now type`。今天會說。

#### M41〔讀碼〕`cloud login` 失敗時說「會自己重試」，但它不會｜U8
- **在哪**：§6 的 `cloud login`，遇到斷網、等滿 10 分鐘、或 code 過期。
- **原字**：`what to do The control plane or the relay could not be reached. The line retries by itself; check the network, any proxy, and the system clock.`
- **為什麼幫不了**：`login` 跑一次就結束，不會重試。等待中只要有一次 poll 遇到網路抖動，整個 login 就中止了。方案滿了的時候，「再跑一次」也只會再失敗一次。
- **位置**：`internal/adapters/cloud/failure.go:100`、`:102`；`internal/adapters/cloud/account.go:147-149`；`cmd/clawdline/cloud.go:268-279`
- **應該**：login 的情境用自己的補救說明：「檢查網路之後，再跑一次這個指令」。poll 遇到暫時性的錯誤要重試。
- **紅燈**：斷網跑 `cloud login`，輸出不能含 `retries by itself`。

#### M42〔讀碼〕`cloud revoke` 對只在帳號清單上的裝置回「nothing changed」，裝置照樣被放行｜U8
- **在哪**：§6「to remove one」，對 `cloud devices` 裡標著 `roster only` 的那一列。
- **原字**：`nothing changed: dev_9a1b… was not a browser this machine had pinned`
- **為什麼幫不了**：`Revoke` 只處理本機釘選的裝置，而 `publicKeyFor` 最後還是會用 roster 的金鑰放行。這句話只說了什麼都沒發生，沒說要怎麼真的移除。
- **位置**：`cmd/clawdline/cloudpair.go:180-182`；`internal/adapters/cloud/pinned.go:247-254`；`internal/transport/cloud/link.go:613-616`
- **應該**：對 roster-only 的 id 也寫一筆本機拒絕紀錄，或明說要去 app.clawdline.com 移除。
- **紅燈**：對 roster-only 的 id 跑 revoke，之後這個 id 的 envelope 要被拒。今天不會。

#### M43〔讀碼〕port 被自己的 daemon 佔住，卻建議「換一個 port」｜U9
- **在哪**：§2，在第二個終端機又跑了一次 `serve`。
- **原字**：`… Stop pid 4242 by its PID if it should not be there, or give this daemon another port with CLAWDLINE_NEXT_PORT.`
- **為什麼幫不了**：佔住 port 的正是同一個產品、同一個狀態目錄。只換 port，就會有兩個 daemon 寫同一個 store、同一個 broker、同一個 Cloud 身分。狀態目錄也沒有 lock。
- **位置**：`cmd/clawdline/portheld.go:92-96`；`docs/getting-started.md:214`
- **應該**：佔用者是 clawdline-go 時，說「這裡已經有一個 Clawdline 在跑，直接用它：`clawdline open`。要跑第二個，需要自己的 `CLAWDLINE_NEXT_DIR`，不只是換 port。」
- **紅燈**：同一個 `CLAWDLINE_NEXT_DIR` 跑第二個 `serve`，stderr 不能只建議換 `CLAWDLINE_NEXT_PORT`。

#### M44〔讀碼〕Mac app 內建的 daemon 起不來時，視窗叫人去跑 `clawdline serve`｜U9
- **在哪**：§5 第一次開 app，內建的 daemon 結束了（store 打不開、Intel Mac 上 `Bad CPU type`）。
- **原字**：`The daemon is not answering`／`Start it with clawdline serve. This window tries again by itself; ⌘R tries now.`
- **為什麼幫不了**：文件寫的是「You do not run `serve` yourself」，而 binary 在 bundle 裡、不在 `PATH` 上。shell 其實知道 daemon 的 exit status，但只寫進自己的 log。
- **位置**：`shell/darwin/main.swift:1056-1063`、`:395`；`shell/darwin/Daemon.swift:33-47`；`tools/package-macos.sh:19-21`
- **應該**：說「這個 app 內建的 daemon 已經結束（status N），原因在 `~/.config/clawdline-next/logs/daemon.log`」，附一顆重新啟動的按鈕。打包腳本在非 arm64 上直接拒絕。
- **紅燈**：把 bundle 裡的 daemon 換成一個會 `exit 1` 的檔案，視窗不能出現 `clawdline serve`。

#### M45〔讀碼〕7727 被一個沒有 console 的 daemon 佔住時，app 視窗顯示原始 JSON｜U9
- **在哪**：§5「If a daemon you started by hand already holds port 7727 … the window shows the one already there」。
- **原字**：`{"detail":"this daemon was not told where the console is; …","error":"no_web_root"}`
- **為什麼幫不了**：501 被當成導覽成功，shell 不看 HTTP status。daemon log 已經寫了「serves no console」，視窗卻不說。
- **位置**：`shell/darwin/main.swift:535-542`；`docs/getting-started.md:147-148`
- **應該**：`/` 不是 200 時說「7727 上的 daemon（pid N）沒有 console，把它停掉，讓 app 自己的 daemon 接手」。
- **紅燈**：先用沒設 WEB 的 `serve` 佔住 7727，再開 app，視窗不能出現 `"error":"no_web_root"`。

#### M46〔讀碼〕hosted：改名成功了，清單卻永遠不會出現新名字｜U10
- **在哪**：機器那一列的 ✎ → 輸入新名字 → 改名。
- **原字**：`{machine} 在這個帳號上現在叫做 {name}。`、`這份清單是各台機器自己上次說的，所以在這一台重新回報之前，它還是會顯示舊的名字。`
- **為什麼幫不了**：清單上的名字來自機器自己 descriptor 裡的 `name`，daemon 從不讀回帳號上的名字；identity 只在 `cloud login` 時存一次。所以「重新回報之前」永遠不會結束。
- **位置**：`next-strings.ts:243`、`:246` → `CloudGate.tsx:736-738`；`internal/transport/cloud/link.go:433-435`；`internal/transport/cloud/name.go:36-46`；`cmd/clawdline/cloud.go:287`
- **應該**：清單優先用控制平面上的名字，或讓 daemon 採用帳號上的名字。在那之前，不要承諾之後會更新。
- **紅燈**：改名成功並讓機器重新發佈 descriptor 之後，`.cloud-machine-name` 要等於新名字。

#### M47〔讀碼〕裝置頁的「開新 Session」不管按哪一台，都只是跳回目前那台的清單｜U10
- **在哪**：在讀機器 A 時進「裝置」，按機器 B 卡片上的「開新 Session」。本機版也一樣，只是跳頁。
- **原字**：`開新 Session`
- **為什麼幫不了**：callback 完全不看傳進來的機器 id，只做 `navigate("sessions")`，開 session 的面板也沒有打開。加第二台機器的人，第一個就會踩到這裡。
- **位置**：`zh-Hant.json:199` → `web/console/src/legacy/js/view/devices.js:141-146`；`web/console/src/pages/devices.tsx:39`
- **應該**：是別台機器時，先切過去再打開開 session 的面板；做不到就不要畫這顆按鈕。
- **紅燈**：讀 A 時按 B 的 `.device-start`，最後讀的要是 B，而且 `#start` 要是打開的。

#### M48〔讀碼〕從 console 開 session 被拒時，要去改系統設定的原因一律說成「開不起來」｜U7
- **在哪**：＋ → 選一個專案 → 被拒。
- **原字**：`這個開不起來。（iterm_attention_required）`；`terminal_io_failed`、`capability_unavailable`、`terminal_busy`、`internal` 也都顯示同一句。
- **為什麼幫不了**：`iterm_attention_required` 其實是 macOS 沒有給自動化權限，他得自己去系統設定打開，但畫面沒說去哪裡。
  `terminal_busy` 只要等幾秒就好，卻寫得像故障。
- **位置**：`zh-Hant.json:728`；`web/console/src/session/Start.tsx:150-163`（`ownWhy` 只處理 6 個代碼）；`internal/app/start.go:154-161`
- **應該**：每個代碼各有一句。例如 `iterm_attention_required` 寫「允許 Clawdline 控制 iTerm2（系統設定 → 隱私權與安全性 → 自動化）」；`terminal_busy` 寫「其他 session 正在開，幾秒後再試」。
- **紅燈**：開 session 回 `iterm_attention_required` 時，說明列不能是 `這個開不起來`。

### 4.3 粗糙（難看，但走得下去）

每筆的欄位和前面一樣，只是寫得短一點。

#### R01〔讀碼〕照抄 getting-started 檢查重啟的 `grep`，一定找不到檔案｜U1
- **在哪／原字**：§2「Check a restart by what it was for」，`grep -A1 'listening on' logs/daemon.log | tail -2`
- **為什麼**：log 在狀態目錄裡，但照前面的步驟走，人這時在 checkout 目錄。照抄只會得到 `No such file or directory`。
- **位置**：`docs/getting-started.md:92`
- **應該**：寫完整路徑，`~/.config/clawdline-next/logs/daemon.log`，或用 `"$CLAWDLINE_NEXT_DIR/logs/daemon.log"`。
- **紅燈**：在 checkout 目錄照抄第 92 行，要印出 `console:`。

#### R02〔實測〕最上層的 usage 沒列出文件要用的 cloud 子指令｜U1
- **在哪／原字**：直接跑 `clawdline` 或打錯指令時印出的 usage：`cloud <status|on|off|login|connect>   the line to app.clawdline.com; off by default`
- **為什麼**：getting-started §6 用到的 `pair`、`commands`、`devices`、`revoke` 都不在這一行。只打 `clawdline cloud` 的話，會印出完整清單。
- **位置**：`cmd/clawdline/main.go:322`（完整清單在 `cmd/clawdline/cloud.go:84`）
- **應該**：直接用 `cloudUsage()` 的那一行。
- **紅燈**：`clawdline 2>&1 | grep -c 'cloud.*pair'` 要 ≥ 1。今天是 0。

#### R03〔讀碼〕hosted：iPhone 的安裝畫面只說要做什麼，沒說怎麼做｜U3
- **在哪／原字**：第一次用 iPhone Safari 打開頁面時，看到 `請先把 Clawdline 加到主畫面`，以及「…請把這一頁加到主畫面，從那裡打開，在那裡登入。」
- **為什麼**：沒提「分享 → 加入主畫面」這個操作。機器設定頁其實寫了（`settings/window/copy.ts:179`），手機這邊卻沒有。
- **位置**：`next-strings.ts:210-211` → `CloudGate.tsx:808-814`
- **應該**：寫「點 Safari 下方的『分享』→『加入主畫面』」。
- **紅燈**：`data-cloud-screen="install"` 的文字要含「分享」和「加入主畫面」。

#### R04〔讀碼〕即時畫面不管怎麼失敗，都只說「讀不到那個畫面」｜U4-B
- **在哪／原字**：⋯ →「即時畫面」，看到 `讀不到那個畫面`
- **為什麼**：失敗被壓成一個 `setFailed(true)`，代碼就丟了。`refusals/scan.ts` 只抓「在 handler 裡說話」的形狀，抓不到這種只設布林值的寫法。
- **位置**：`zh-Hant.json:653`；`web/console/src/session/ScreenPanel.tsx:122-127`、`:192-196`、`:206-212`
- **應該**：保留錯誤本身，交給 `failureSentence`；守衛也要抓「只設布林值」的形狀。
- **紅燈**：screen 回 404 `session_not_found` 時，面板要帶出這個代碼。

#### R05〔讀碼〕沒裝 git，Git 面板卻說「無法讀取」｜U4-B
- **在哪／原字**：⋯ →「Git 變更」，看到 `無法讀取 Git 變更（git_unavailable）`
- **位置**：`zh-Hant.json:278`；`web/console/src/legacy/git-bridge.ts:134-139`；`internal/transport/http/git.go:78-80`
- **應該**：寫「這台機器沒有安裝 git」。
- **紅燈**：回 501 `git_unavailable` 時，面板不能含 `無法讀取`。

#### R06〔讀碼〕「現在」頁在 daemon 不通時，每一塊都寫 `unexpected_error`｜U4-B
- **在哪／原字**：「現在」頁，看到 `這一塊讀不到。（unexpected_error）`
- **為什麼**：`TransportError` 沒有代碼，成因和 M12 一樣。
- **位置**：`web/console/src/pages/now/shared.ts:24-25`；`web/console/src/pages/now.tsx:90`、`:105`、`:116`
- **應該**：把 `TransportError` 標成 `offline`。
- **紅燈**：fetch 被 reject 時，文字仍然含 `unexpected_error`。

#### R07〔讀碼〕工作看板的錯誤代碼講兩次｜U4-B
- **在哪／原字**：`讀不到看板。 沒有成功：store_unavailable（store_unavailable）`
- **為什麼**：「現在」頁修過一模一樣的問題（`now/shared.ts:14-18`），看板沒跟著修。
- **位置**：`web/console/src/pages/work/shared.ts:81`；`web/console/src/pages/work/words.ts:256`
- **應該**：照 `now/shared.ts:25` 的做法，fallback 不帶代碼。
- **紅燈**：同一個代碼出現兩次。

#### R08〔讀碼〕hosted：讀機器清單時的任何錯誤，都被說成「沒有機器」｜U4-C
- **在哪／原字**：機器清單、裝置頁，看到 `這個帳號還沒有任何機器回報。`、`目前沒有可用的機器。`
- **為什麼**：`machines()` 被拒時不看原因，一律換成空清單。
- **位置**：`CloudGate.tsx:212-215`（→ `:931`）；`web/console/src/legacy/js/view/devices.js:178-181`
- **應該**：只有 `cloud_read_unavailable` 才說「沒有機器」，其他錯誤要說出原因，並給重試。
- **紅燈**：讓 `client.machines()` 以其他代碼拒絕時，畫面不能出現 `這個帳號還沒有任何機器回報`。

#### R09〔讀碼〕hosted：裝置卡片寫「0 Session 清單」｜U4-C
- **在哪／原字**：抽屜 →「裝置」，看到 `3 Session 清單`、`0 Session 清單`
- **為什麼**：`webSessions` 其實是頁面標題，卻被接在數字後面。沒配對的卡片也照樣畫 0。
- **位置**：`zh-Hant.json:664` → `web/console/src/legacy/js/view/devices.js:46-47`
- **應該**：改用 `{count} 個 session`；不知道數量就不寫。
- **紅燈**：`sessions:3` 的卡片，文字不能是 `3 Session 清單`。

#### R10〔實測〕專案頁和開 session 面板，把 worktree 目錄的 UUID 當專案名字｜U5
- **在哪／原字**：派過一次 `isolation: "worktree"` 的工之後，側欄「專案」和 ＋ 面板都會出現一列 `3f9a2c1b-…-…`（UUID，這裡是舉例），下一行是它的路徑 `…/worktrees/<repo>-<hash>/3f9a2c1b-…`
- **為什麼**：broker 用 task id 當 worktree 的目錄名，而專案名取的是目錄的最後一段。派過幾次工，專案清單就是一排 UUID。
- **位置**：`internal/adapters/projects/places.go:93`（`filepath.Base(path)`）；worktree 路徑是 `<CLAWDLINE_NEXT_DIR>/worktrees/<slug>/<id>`（`docs/broker.md:46`）
- **應該**：worktree 目錄顯示「原 repo 名 · task 標題」，或併到原 repo 那一列底下。
- **紅燈**：派出一個 worktree child 之後，`/v1/places` 裡不能有 `name` 是 UUID 的列。

#### R11〔讀碼〕`cloud login`、`status`、`pair` 印 id 不印名字｜U5
- **在哪／原字**：`account    0b6f…`、`machine    7d1e…-c0a3`、`paired     dev_4c2a…`、`verified   the control plane issued a device token (expires …)`
- **為什麼**：app.clawdline.com 上看到的是機器名稱，CLI 印的是 id，兩邊對不起來。「expires」那一行讀起來像是有東西快過期、要處理。
- **位置**：`cmd/clawdline/cloud.go:161-162`、`:291-292`、`:300`；`cmd/clawdline/cloudpair.go:120`
- **應該**：印名字，例如 `registered   "alex-mbp" on your account`。pair 的名字從 roster 查。
- **紅燈**：`cloud login --name test-box` 成功之後，輸出要含 `test-box`。

#### R12〔讀碼〕hosted：帳號和瀏覽器都用原始 id 表示｜U5
- **在哪／原字**：機器清單標題下面的 `帳號 {account} · 這個瀏覽器 {device}`，實際畫出來是 `帳號 acct_x1y2z3 · 這個瀏覽器 web_q9w8e7`（id 為舉例）
- **位置**：`next-strings.ts:213` → `CloudGate.tsx:866`；`next-strings.ts:208` → `:838`
- **應該**：顯示登入身分和裝置種類（例如「這支 iPhone」）；不知道就整句省略。
- **紅燈**：machines 畫面的 `.fine` 不能含原始的 `who.account` 字串。

#### R13〔讀碼〕工作看板和 Dashboard 用 id 當名字｜U5
- **在哪／原字**：`由交付 3f9a2c1b 提出——…`、轉交欄位的 `交給誰（session id，或 user）`、關閉確認鈕的 `確定關掉 %12`
- **位置**：`web/console/src/pages/work/Board.tsx:446`（字串在 `work/words.ts:177`）、`:230-231`（`words.ts:243`）；`web/console/src/Dashboard.tsx:632`
- **應該**：用任務標題和 session 的 label；轉交改成從 live sessions 裡挑。
- **紅燈**：卡片或按鈕上出現 8 碼十六進位或 `%N`。

#### R14〔讀碼〕裝置頁的本機卡片畫出 `this-mac` 晶片，Linux 上也一樣｜U5
- **在哪／原字**：`this-mac`；說明文字是 `只有服務這一頁的那一台機器。daemon 服務的 console 連不到別台；…`
- **位置**：`web/console/src/legacy/js/view/devices.js:39`、`:120-122`；`web/console/src/legacy/js/session/selection.js:11`；`next-strings.ts:254-255`
- **應該**：不畫這個 id 晶片；說明改用白話。
- **紅燈**：Linux 上的頁面仍含 `this-mac`。

#### R15〔實測〕「用量」頁幾乎全是英文｜U6-a
- **在哪／原字**：側欄「用量」，看到 `Generated output is an operational signal, not a productivity score.`、`Project Portfolio`、`Back to sessions`、`Unknown`
- **為什麼**：這一頁的 markup 是直接從舊 app 的 `index.html` 搬過來的，光是 `section.html` 就有 34 行英文。看不懂英文的人，在這一頁其實走不下去。
- **位置**：`web/console/src/pages/usage/section.html:4-6`、`:28`；`web/console/src/legacy/js/view/usage.js:134`、`:138`、`:164`、`:166`
- **應該**：字串進字典；翻完之前先把這一列從側欄拿掉。
- **紅燈**：zh 介面下，`#usage-analytics` 仍含 `Project Portfolio`。

#### R16〔讀碼〕Dashboard 的英文標題與原始 enum｜U6-a
- **在哪／原字**：側欄那一列寫 `Dashboard`；面板標題是 `Sessions`、`Obligations`、`Tasks`、`Schedules`、`Coordinator`；內容直接印 `state / evidence`、`gen 3`
- **位置**：`web/console/src/App.tsx:787`；`web/console/src/Dashboard.tsx:336`、`:374`、`:385`、`:411-412`、`:439`、`:468`、`:508-509`、`:523`
- **應該**：改用中文字串；錯誤一律經過 `failureSentence`。
- **紅燈**：zh 介面下，側欄那一列的文字仍是 `Dashboard`。

#### R17〔讀碼〕排程區塊的標題是英文，空的時候也沒有說明｜U6-a
- **在哪／原字**：`Schedules`；表單裡的 `Model`、`Delete`、`Cancel`
- **位置**：`web/console/src/pages/schedules.tsx:1581`；`web/console/src/pages/schedules/overlays.html:118`、`:130`、`:150`
- **應該**：標題改成「排程」；空的時候寫「還沒有排程。按 ＋ 設定一個。」
- **紅燈**：`#schedules summary` 仍是 `Schedules`。

#### R18〔讀碼〕文件頁「分享」的灰色按鈕，hover 說明是英文，而且寫錯了｜U6-a
- **在哪／原字**：`document_share_unavailable: The document could not be read.（document_share_unavailable）`
- **為什麼**：文件其實讀到了。這句說明只有 hover 才看得到，手機上等於沒寫。0 個 session 時，這一頁根本進不去。
- **位置**：`web/console/src/legacy/js/view/documents.js:23-27`、`:155-158`；`web/console/src/pages/documents/section.html:18-19`
- **應該**：本機版不畫這兩顆按鈕。
- **紅燈**：`#document-share` 的 title 含英文。

#### R19〔讀碼〕hosted：新使用者看不懂的術語和英文｜U6-a
- **在哪／原字**：「裝置」一詞同時指機器和瀏覽器；`routing 會立刻停止。但已經握有主金鑰的裝置…`；`它不回答 past-sessions`；`伺服器回報 \`write: false\``；`串流上還沒有東西進來`；推播失敗時的 `[server.subscribe: push_timeout]`
- **位置**：`zh-Hant.json:207`、`:242`、`:778`；`next-strings.ts:206`、`:209`、`:229`（→ `CloudGate.tsx:628`）、`:259-260`；`web/console/src/push/push.ts:340`
- **應該**：「機器」和「瀏覽器／手機」分開用詞；密碼學細節收進「詳細說明」。
- **紅燈**：zh 介面的 DOM 仍含 `routing`、`write: false`、`past-sessions`、`[server.`。

#### R20〔讀碼〕「現在」頁和工作看板的術語｜U6-a
- **在哪／原字**：`交了還沒記帳`、`root：{root}`、`另一個 store 用的是上一次的讀取…`
- **位置**：`web/console/src/pages/now/words.ts:84-85`、`:88-89`、`:100`、`:115`、`:117`；`web/console/src/pages/work/words.ts:194`、`:258`
- **應該**：改用白話；store 是空的時候，說明東西會怎麼進來。
- **紅燈**：zh 字串仍含 `root`、`store`、`daemon`。

#### R21〔讀碼〕驗證帳本的空狀態，是一句術語死句｜U6-a
- **在哪／原字**：`這台 Mac 上還沒有任何 Feature 有複審或驗證收據。`
- **位置**：`zh-Hant.json:393`、`:400`；`web/console/src/legacy/js/view/ledger.js:310`
- **應該**：用一句話說明這頁只記錄帶 `graph` 的派工，並附上怎麼產生第一筆。
- **紅燈**：畫面上沒有任何一個能產生第一筆紀錄的指引。

#### R22〔讀碼〕方案頁的標題底下是空白｜U6-b
- **在哪／原字**：本機的方案頁，標題 `這個方案包含什麼` 下面什麼都沒有
- **為什麼**：limits 被藏起來了，標題卻照畫，看起來像載入失敗。
- **位置**：`web/console/src/pages/plan/section.html:29-30`；`web/console/src/legacy/js/view/plan.js:170`、`:233`
- **應該**：標題跟著一起藏。
- **紅燈**：`#plan-includes` 看得到，但 `#plan-limits` 是 hidden。

#### R23〔讀碼〕桌面版右半邊叫人「在左邊挑一個」，但左邊是空的｜U7
- **在哪／原字**：0 個 session 的桌面版：`沒有打開的 session`／`在左邊挑一個 session。`；輸入框下方 `先打開一個 session 才能寫進去。`
- **位置**：`zh-Hant.json:452`、`:494`、`:779`；`web/console/src/session/Detail.tsx:133`、`:249-250`；`web/console/src/session/StatusLine.tsx:94`；`web/console/src/session/Composer.tsx:427`
- **應該**：清單是 0 列時，右半邊改成和 M34 同一套開始指引，加一顆「開一個 session」。
- **紅燈**：清單 0 列時，畫面仍出現 `在左邊挑`。

#### R24〔讀碼〕語音：沒裝 Whisper 是一句死句，而且錄完才說｜U7
- **在哪／原字**：麥克風 → 錄一段 → 停止，看到 `Mac 上沒有裝 Whisper，那邊沒有東西可以拿來轉文字。（no_whisper）`
- **為什麼**：沒說要裝的是 `whisper-cli`，也沒說怎麼裝；Linux 上一樣說「Mac」；而且要錄完才告訴他。
- **位置**：`zh-Hant.json:764`；`web/console/src/legacy/js/core/failure-text.js:92-94`；`internal/adapters/whisper/whisper.go:45`
- **應該**：打開頁面時就知道有沒有 whisper。沒有的話停用麥克風，並附一句怎麼裝。
- **紅燈**：沒有 `whisper-cli` 時，麥克風按鈕仍然可以按。

#### R25〔讀碼〕專案清單的空狀態，只陳述事實｜U7
- **在哪／原字**：`這台 Mac 還沒有任何專案目錄的紀錄。`
- **位置**：`zh-Hant.json:589`；`web/console/src/legacy/js/view/projects.js:451`
- **應該**：沿用 M34 的指引。
- **紅燈**：`/v1/places` 回空清單時，文字裡沒有 `claude` 或 `codex`。

#### R26〔讀碼〕daemon 沒在跑時，cloud 子指令和 `tunnel` 吐出 Go 的原始錯誤，而且沒有 timeout｜U8
- **在哪／原字**：`clawdline: Get "http://127.0.0.1:7727/v1/cloud/status": dial tcp 127.0.0.1:7727: connect: connection refused`
- **為什麼**：troubleshooting 表教人比對 `the daemon did not answer` 這句，但只有 `open` 和 `pair` 會印它。這些指令用的是 `http.DefaultClient`，沒有設 timeout，daemon 卡住時指令會一直掛著。
- **位置**：`cmd/clawdline/cloudpair.go:44-47`；`cmd/clawdline/tunnel.go:28-31`；`docs/getting-started.md:215`
- **應該**：共用 `open` 的寫法，並加上 10 秒 timeout。
- **紅燈**：daemon 沒在跑時，`cloud devices` 的 stderr 要含 `the daemon did not answer`。

#### R27〔讀碼〕`cloud pair` 失敗時丟掉型別化的原因｜U8
- **在哪／原字**：`… no such host (pairing_failed)`；`the control plane refused this pairing: the cloud credential was refused (pairing_refused)`；`the pairing did not complete: that pairing invitation has expired or was already used`
- **為什麼**：網路問題被當成 HTTP 400。憑證被撤銷時沒說要重新 login；連結過期時也沒說「再跑一次 `cloud pair`」。
- **位置**：`internal/transport/http/cloud.go:305-318`；`internal/adapters/cloud/pairing.go:208-209`；`cmd/clawdline/cloudpair.go:125`
- **應該**：pairing 的錯誤也走 `cloudFail`，印出 `what to do`。
- **紅燈**：斷網時跑 `cloud pair`，輸出要含 `what to do`。

#### R28〔讀碼〕沒設 `CLAWDLINE_NEXT_WEB` 時，只說變數名，不給值｜U9
- **在哪／原字**：`console: NONE — this daemon was not told where the console is: CLAWDLINE_NEXT_WEB is not set, so / answers 501 no_web_root …`
- **為什麼**：binary 在 `bin/` 裡，console 就在 `../web/console/dist`，daemon 找得到卻不去找，也沒給一個可以直接複製的路徑。
- **位置**：`internal/transport/http/page.go:28-33`、`:68-69`、`:77-78`
- **應該**：先試 `<執行檔>/../web/console/dist/index.html`。找不到的話，才印出一個可以直接複製的完整指令。
- **紅燈**：在 checkout 裡不設這個變數跑 `serve`，`GET /` 要是 200，或 NONE 那一行要含絕對路徑。

#### R29〔讀碼〕hosted：回到機器清單的唯一入口，在觸控裝置上看不出來｜U10
- **在哪／原字**：console 右上角的機器名稱 pill；「換一台機器」只寫在 `title` 屬性裡
- **為什麼**：手機沒有 tooltip。第二台機器加進帳號時，console 裡也沒有任何提示。
- **位置**：`next-strings.ts:220` → `CloudGate.tsx:462-472`；`web/console/src/cloud/cloud.css:19-25`
- **應該**：加上看得見的「換機器」字樣或圖示；帳號出現新機器時給一則提示。
- **紅燈**：觸控環境下，`#cloud-switch` 看得見的文字不含「換」，也沒有圖示。

## 5. 要他拍板的產品決定

**全部拍板了，2026-09-21。答案在每一條下面。** 以下四件不是技術待辦，每一件都會改變對應單位要怎麼做。

- **D1 公開的程式碼放哪裡**（決定 B01、U1 怎麼做）
  - 選項 A：把 `sainteye/clawdline` 換成這個 Go repo，Swift 版封存成另一個名字。
  - 選項 B：公開 `sainteye/clawdline-go`，並改掉所有文件裡的網址。

  兩條路都要同時處理 `go.mod`、README、repo 描述。公開之前，AGENTS.md 要求先跑一次
  `tools/check-private.sh -history -full -revs=--all`，而目前 history 裡已知有三行紅的（`ef067d70`）。
  **拍板：A。** `sainteye/clawdline` 換成這個 Go repo；Swift 版保留成 `swift` 分支與 `swift-final` tag，不刪。
  歷史清洗的範圍同時拍板：`private-word`（事業名）、`private-repo`（私有 cloud repo 的名字）、`home-path`、`ip-address`，
  共 39 個相異位置；`pane-id`／`task-id`／`uuid`（469 處）不動，它們是這台機器的終端機代號與派工編號。

- **D2 舊 app 的字典和移植頁，還要不要逐位元組鎖定**（決定 U6，以及 U2、U3、U4 裡所有改字的做法）
  - 繼續鎖：每一句修正都只能在 `next-strings.ts` 或各頁的 `words.ts` 疊一層覆寫，
    `check-machine-words.sh` 要改成掃「畫面最後吃到的字」。
  - 解鎖：直接改 `zh-Hant.json` 和 legacy HTML，`check-legacy-css.sh` 對它們不再比對位元組。
  **拍板：繼續鎖。** 解鎖會讓 `~/code/clawdline` 那份對照失去意義，而它是判斷「哪些功能還沒 migrate」的唯一依據。
  所以每一句修正都疊一層覆寫，而 `check-machine-words.sh` 要改成掃「畫面最後吃到的字」。

- **D3 `clawdline open` 預設給不給打字權限**（決定 B08 怎麼修）：新使用者開 console 的第一件事就是想打字。
  現在的預設是唯讀，而且畫面上完全看不出來。
  **拍板：預設可以打字。**

- **D4 產生資料的那一端不存在的移植頁，要隱藏還是補上**（決定 U6-b）：舊看板、帳本回填、時間軸的 production 證據、Feature 歸屬。

  **拍板：交給實作判斷，逐頁決定。** 判準是哪一個對第一次來的人比較誠實——
  一頁宣稱有資料而畫不出來，比沒有那一頁糟。

## 6. 和 in-flight 工作的重疊

查的是 broker 的 `/inflight`，時間在這一輪開始時。

| task | 狀態 | 和這份清單的關係 |
| --- | --- | --- |
| `fd2d006e` 在 PWA 裡沒有任何方法可以配對一台新機器 | 進行中 | B09 大部分、B11 的一部分；派 U3 之前先等它落地，或併進它 |
| `69e8e237` 剛開的 Codex 認不出來：沒有 resume 就沒有身分 | 已交付，未落地 | 可能改到 M11、M24 上游的 identity；U5 要在它落地後再量一次 |
| `620c9aee` 側欄缺的兩頁：驗證帳本與專案時間軸 | 已交付，未落地 | 可能改到 M28、M29、R21 |
| `cf0e4585` 額度說 unknown 的時候要說出為什麼 | 已交付，未落地 | 和 U4 是同一家族（原因走到畫面），做法可以共用 |
| `16104842` 還在照舊 app 的規則走的地方，要改成照現在的 | 已交付，未落地 | 和 U6、M02、M03 同一家族 |
| `31c4bd21` 在 AWS 的 Linux 上把新版從頭跑通一次 | 已交付，未落地 | 改的是 `docs/linux.md`；U1 做完後，兩份文件要互相對得上 |
| `f47994be` 語音轉寫預設吐簡體 | 進行中 | 會動到 `internal/adapters/whisper`，R24 別同時動 |
| `e1752acd` Cloud 的詞彙表少兩個字：past-sessions 與排程寫入 | 已交付，未落地 | 會讓 R19 的 `它不回答 past-sessions` 變少 |
| `3ace5aa8` 使用者問了三次「現在到底是什麼狀況」 | spawn_failed | 和 R06、R20（「現在」頁）同一塊，還沒有人在做 |

## 7. 已修好但尚未上線的項目（待檢視）

沒有。這一輪只做盤點，沒有改任何產品程式碼；這份報告本身是唯一的變更。
