---
name: clawdline
version: 2.2.0
description: |
  把工作派給另一個 session 做：透過 Clawdline app 開一個 child session（Claude 或 Codex），
  注入第一句話、等它寫回 result.json，完成時回報給你。適合「這件事我不想在這條對話裡做」
  的雜活——生圖、跑測試、審一份 diff、長時間的整理。也負責把**整條線**交出去——Clawdline
  handoff：把這條對話的狀態寫成一份文件，開一個 session 接著做（§7）。
  觸發時機：使用者說「派任務」「派給 codex 做」「開一個 child／子 session」「背景幫我做 X」
  「dispatch 一個任務」「另開一個 session 去跑」「用 codex 生一張圖」「叫另一個 agent 去審」，
  或一次要平行做好幾件彼此不相干的事；handoff 那半邊則是「使用 Clawdline Handoff」
  「handoff 給新 session」「交接給下一個 session」「明天用新 session 接著做」，
  以及同樣意思的英文說法 "use Clawdline Handoff"、"hand this over to a fresh session"、
  "pick this up in Codex"、"continue this tomorrow in a new session"。
  不要觸發：這條對話自己動手就好的小事（開 child 的成本遠大於直接做）、Task/subagent 就能解決
  的檢索與分析（那是 subagent，不是 Clawdline child）、單純想知道現在有哪些 session 在跑
  （那是看 Clawdline 面板或 GET /v1/orchestrator/sessions）。
  **這個 session 自己就是 child 時，依據是 CHILD.md 不是這裡**——見 §0。
user-invocable: true
last-updated: 2026-08-26
---

> 這是英文正本 [`SKILL.md`](SKILL.md) 的繁體中文對照版，內容同步。裝哪一份都可以，
> 但同一台機器只裝一份——兩份的 `name:` 都是 `clawdline`。

# 派任務給 child session

你現在是 **Root**。Clawdline app 是 **broker**：你寫檔、按一下 HTTP，它去終端機開一個新分頁、
把第一句話打進去、盯著完成、算 token、回頭通知你。**Child** 是被開出來的那個 session，
它只做一件事，做完寫 `result.json`。

整條路只有六步，照順序做完就對了。

這六步派出去的是一件**任務**。把**整條線本身**交出去——這條對話累積的狀態，交給另一個 session
接著做——是另一回事、另一套規矩，在 §7。

---

## 0. 先確認你在哪一層——這條決定你能不能往下派

**下面任何一條成立，你就是 child：**

- 這條對話的**第一句**是 `You are a Clawdline CHILD agent for task <id>…`
- 你讀過、或被要求去讀 `/tmp/.clawdline/<id>/CHILD.md`
- 你手上有一個 `TASK_SECRET=`

**你是 child 的話，依據不是這份 skill，是 `CHILD.md`。** 去讀它的「Handing work on」那一段：

- **那一段在** → 照它做就好。它已經把整份指令寫死給你了，包括 `root.parent_task` 要填你自己那件
  task 的 id——那是這裡唯一沒有人會替你填的欄位。下面的 §1–§6 只是同一件事的長版，可以參考，
  但衝突時以 `CHILD.md` 為準。
- **那一段不在** → 你這一層已經是底。**立刻停止**，告訴使用者「這個 session 已經在最底層，
  不能再往下派」，然後自己把事情做掉。

這棵樹只有兩層：使用者的 session 派 child，child 再派一層，就沒有了。沒有底的話，
一件事變五件變二十五件，一台 Mac 開到爆。app 端有擋（dispatch 會回 `depth_exceeded`），
但那是最後一道，不是第一道——**第一道是你自己**。

---

## 1. 找到 port 和 token

```bash
PORT=$(jq -r '.remote_port // 7717' ~/.config/clawdline/config.json 2>/dev/null || echo 7717)
TOKEN=$(cat ~/.config/clawdline/orchestrator-token 2>/dev/null)
[ -n "$TOKEN" ] || echo "NO TOKEN"
curl -s "http://127.0.0.1:$PORT/v1/health"
```

- `NO TOKEN`／檔案不存在 → **停下來**，告訴使用者：Clawdline 沒開，或版本太舊沒有 orchestrator。
  請他開 Clawdline、到 Settings → Remote 打開 **Answer over HTTP**，token 會在 server 起來時自己生出來。
- `curl` 連不上 → 同上，server 沒在跑。
- health 回得出來但 token 檔沒有 → 這台的 Clawdline 還沒有這個功能，請使用者更新。

**這個 token 是「我是本機、以你的身分在跑的程式」的證明。** 不要寫進任何檔案、不要交給 child、
不要放進 `/tmp`、不要貼進回覆裡。

---

## 2. 先把整張圖畫出來，再決定派給誰

### 2.0 先讀政策，然後先回答「這件事該不該派」

**每一次派工之前，第一個動作是讀這台 Mac 的政策檔：**

```bash
cat ~/.config/clawdline/dispatch-policy.md 2>/dev/null
```

那份檔案**由 app 提供出廠內容、由使用者持續修改**，所以它會隨著這個專案的認識一起長。
**它的優先權高於這份 skill 裡的任何一條。** 兩邊牴觸時照它做，並在回報時說你照了哪一條。
檔案不存在或是空的，才照這份 skill 的預設走。使用者可以在 Settings → Remote →「派工的規矩」
裡編輯它，你也可以在他要求時幫他改。

讀完之後，**先回答政策開頭那個問題：這件事該不該派？**

判準是一句話：**這件事能不能切成幾塊互不相干、各自做完再合起來的工作？** 背後有量測——
能切開的工作，多 agent 比單 agent 好 **80.9%**；每一步都依賴前一步的工作，
**每一種**多 agent 安排都比單 agent 差 **39–70%**，因為交接會把本來該完整的推理鏈打斷。

**答案是「不該派」的時候，那是建議，不是否決。** 講出來、用一句話說明理由、然後**問使用者**，
他說什麼就做什麼。他有這份判斷看不到的理由——可能是想讓 Codex 接這一件、想把自己這條對話的
context 留給別的事、或單純想在一個分頁裡看著它跑。**他說派就派**，不用再勸、也不用附帶條件；
你欠的只是那個理由，而且要在動手之前講，不是事後才說。

反過來也一樣：不要為了「用到這個功能」硬派。最常見的「看起來能派、其實不能」是：
診斷與除錯（每一步都取決於上一步查到什麼）、幾十個細瑣的小工作（每個節點都是一個真的
終端機，開一百個既慢又貴又沒人看得完）、有人在等的即時路徑、需要 agent 之間來回討論的、
產出必須是程式直接吃的結構化資料、以及**寫指令比自己做還久的小事**。

### 2.0b 一件任務該多大，小事什麼時候出去

**一件任務就是一片完整、能獨立審查的 feature slice**——production 改動、先紅後綠的測試、文件、
以及它自己的驗證，由同一個 session 一路做完。實作者留在那片上：同一個 feature 冒出來的新發現或
修正回到同一個 session，不要為每一件開一個新分頁。

**大到值得派的一片，也大到輸得起整片。** `timeout_minutes` 上限 240，助理額度可能中途耗盡，
context 也會滿。所以會跑很久、或會動到好幾個檔的一片，要用 `isolation: "worktree"` 派出去，並且
在指令裡要求它**每完成一個階段就在 delivery branch 上 commit**（不是最後才 commit），工作內容偏離
標題時用 `/progress` 講一句。這樣 child 死在第三個小時只損失一小時——branch 上還留著其餘的。

**絕對不要為了一個小改動開一個 session。** 小事累積成一個池子，符合任一條件就整批派出去：

- 池子裡有 5 件，或
- 這些事加起來超過大約 30 分鐘的工，或
- 其中一件擋住 landing，或有人在等它。

再加一個上限，免得「累積」變成「永遠不做」：**任何一件都不能在池子裡超過 24 小時。** 一件 task
帶整批，`claims` 是整批會寫到的檔案的聯集，指令要把每一件分開列出來，這樣 result 才能逐件回報。

**常駐 session。** session 可以在兩件工作之間不關：一個 **odd-jobs** session 接上面那些批次，
一個 **review** session 接每一片完成的 feature 或每一批。兩者都用 `orchestrator_child_linger: -1`
留住分頁，並在 `kind` 裡寫明角色。但是**工作只能以 attached follow-up task 的形式進到常駐 session**
——那是一筆完整的 task 記錄：自己的 id、secret、`claims`、`timeout_minutes` 和 `result.json`。
`POST /v1/sessions/:id/send` 不算：那是配對裝置的路由，不會產生任何 task 記錄，用它餵進去的工作
沒有 claims、沒有完成訊號、不會出現在 `inflight`、也不算進任何用量。所以**沒有帶 `claims` 的
follow-up task，常駐 session 就完全不能寫共享 tree**——review session 天生符合（它只產出 findings
不產出 bytes），odd-jobs session 不符合。在那個機制進 `HEAD` 之前，誠實的近似作法是「一批出清派
一件 task、一輪 review 派一件 task」；用手打字餵著的分頁不是常駐 session，也不要這樣講。

**被中斷的 review 用交接，不要重跑。** 死掉、逾時或被取消的 reviewer，通常已經寫了一部分
finding set；把那個檔案交給接手的人。review 是這裡最貴的節點，也是被丟掉比例最高的節點——101 次
review 派工裡有 30 次沒交出 verdict，其中一次重審花了 6.7M tokens 去重讀 1.9M tokens、別人已經
讀過的工作。

**reviewer 帶著 findings 回來的時候：** 它要先把完整的 finding set 寫下來回報，**才可以動手修**，
而且只修不改變設計的部分。它一旦寫了 production bytes，verdict 就用掉了——那份修正是一個
delivery，聚焦 diff、mutation、exact-tree 驗收都是你的事，不是它的。會改變設計的修正回原
implementer 的 session。永遠不要一個 finding 派一件 task。

### 2.0a 決定這件任務要不要獨立 worktree

能以 Git branch 審查、收進來的程式修改，才用 `"isolation":"worktree"`。broker 會在任務真的
開始時建立乾淨的私有 checkout；選填的 `isolation_base` 指定 Git revision，不寫就是開始當下的
`HEAD`。純讀的 reviewer、只產 artifacts 的工作、依賴本機未追蹤狀態的工作，不要隔離；真正會撞的
若是跑著的 app、port、裝置、資料庫、cache 或固定 build 目的地，worktree 也幫不上忙，要用
`serialize` 管機器全域資源。

child 的規矩只在這裡窄幅翻轉：共用 checkout 的 child 仍然不 commit；進到自己的 worktree 後則要
早點 commit，而且只能 commit 在 `clawdline/task/<完整-task-id>`。仍然禁止 push、換 branch、merge、
rebase、stash、hard reset、任何 `git worktree`，以及 `build.sh` 這種會動到機器全域安裝的指令。
**交付物是那條 branch，不是 checkout 目錄，也不是 artifact diff。** root 用
`git diff <base>...clawdline/task/<id>` 審，再 merge 或 cherry-pick。worktree 只隔離 tracked files；
gitignore 掉的依賴、cache 與 env 檔不會跟過去。

後續修 findings 的實作輪也是程式修改。下一個 isolated task 要用 `isolation_base` 接在上一輪的
delivery branch 或 commit 上；不要只為了繼續改，就把交付物轉成 artifact-only task 裡的一包 bundle。
每一輪實作都留在 broker 建的 worktree，root 最後要關閉的 task record 才會持續帶著 branch、base、
head、commit 數與 dirty 狀態。

### 2.1 該派的話，先挑一個形狀

政策檔的「pick a shape」一節列了幾個具名的形狀，**挑一個，不要即興**。摘要：

| 形狀 | 什麼時候用 | 節點怎麼配 |
|---|---|---|
| **Split and join** | 一個問題切成幾塊獨立調查 | 葉節點 `haiku` 各查一塊，一個 `sonnet`＋的節點彙整判斷 |
| **Build then read** | **產出是程式碼、或有人會照著做的決定** | 幾個節點做事，**外加一個獨立的審查節點只讀不寫** |
| **Decide then do** | 要改動重要的東西 | 一個節點只定方案不動手 → **人看過** → 另一個節點（通常 codex）實作 |
| **Batch with takeover** | 同一種機械修改跨多個獨立模組 | 一個模組一個節點；死掉的分頁留著讓人接手 |
| **Candidates** | 設計取捨、要比較的是品味 | 幾個節點各做一個完整方案，**人直接挑，不設 judge 節點** |

**寫新功能一律是 Build then read。** 產出程式碼的 child graph，最後一定要有一個獨立審查節點；
root 的工作則要到 §6 的 landing closure 才結束。審查規則在 §2.2 的最後一段。

### 2.2 先畫圖，不要邊派邊想

在送出**任何一件**之前，先把整張圖寫下來：

```
root（你）
├── A  搜 X            claude/haiku   → artifacts/x.md
├── B  搜 Y            claude/haiku   → artifacts/y.md
└── C  彙整 A、B 的產出 claude/opus    → artifacts/report.md
```

要決定的是四件事：**葉節點各自產出什麼、誰負責把它們接起來、每個節點用哪個助理和模型、
最上面要交回什麼**。想不清楚就不要送——一個沒想清楚的 graph，錯誤會在最深的那一層才浮出來。

**廣度優先。** 兩個 child 各做一半，比一個 child 做一半再往下派好：後者多一層延遲、多一次
轉述失真。只有在「第二層要做什麼，非得等第一層答完才講得出來」的時候，才往下走一層。

**產出是程式碼、或是有人會照著做的決定時，child graph 的最後要有一個審查節點；root-owned
graph 不能停在那裡。** 它最後一格必須是 `root：把審過的 delivery 落到 <target>，並驗證整合後
的 tree`。派工前就把這一格、delivery branch、target branch 與 root landing owner 寫進 `plan`。
reviewer 不是第五個工人，是一個讀者：只讀別人的產出、只寫「哪裡有問題」，不動手修（修是下一輪
或人的事，就算它確定知道怎麼改也一樣——順手修掉等於把人本來該看到的判斷埋了）。五條規則：

1. **它沒有參與生產。** 自審是量測過的差：模型審自己的產出會漏掉約三分之一的語意漂移，
   而且機制是結構性的不是能力問題——judge 偏好低困惑度的文本，而模型自己的輸出對它自己
   必然是低困惑度。**更強的模型不會修好這件事。**
2. **換一個助理有幫助，但不等於解決。** codex 寫的給 claude 讀是對的，但別誤以為那就叫獨立：
   九個 frontier 模型組成的 panel，實測只有約兩票的獨立資訊，因為不同模型會在同一題上犯同樣
   的錯。真正重要的審查要**派好幾個審查者取多數**，而且挑「互補」的而不是隨便挑個不同的。
3. **審查一律用 opus 等級的模型。** 不是「不弱於被審者」，是絕對下限。審查的價值完全等於
   審查者的判斷力，而漏掉的問題會一路傳到最後。實測過：一個 sonnet 審查節點在正確說明
   「judging 本身會 hallucinate」的同一份裁決裡，編造了一個具體引用——宣稱某份文件在質疑
   某個術語，而那份文件根本沒提過那個詞。
4. **指令裡要指名它可以讀哪幾個 `/tmp/.clawdline/<id>/artifacts/`。** 這是把第 1 條真的執行
   出來的方法：不指名，審查者就可能翻到它本來該被隔開的生產過程。
5. **要結論，而且要附出處。**「有什麼問題、最嚴重的在前面、這樣能不能出」，每一條問題都要
   指名它依據哪份 artifact 的哪一段。沒有出處的裁決，正是一個在 hallucinate 的 judge
   會產出的形狀。

**每個節點都要收到整張圖**，不是只有自己那一格——這就是 `task.json` 的 `plan` 欄位。
知道自己的產出要餵給誰的葉節點，會寫出接得起來的東西；不知道的會寫一篇心得。

### 2.3 件數

一個 session 預設同時最多 5 個 child（`orchestrator_max_children`，1…10）——這個數字是
**每個 session 各自算的**，不是整台 Mac 算的。你自己就是 child 的話，你的額度是 3
（`orchestrator_max_grandchildren`，0…10；`0` ＝ 不能往下派）。超過會回 `over_capacity`。

### 2.4 哪個助理

使用者講明了就照講的做。沒講的話：

| | 給它 | 因為 |
|---|---|---|
| **codex** | 寫程式、**生圖**、手寫 SVG、跑 build 到綠、大量機械性改檔 | 它擅長「做出一個看得到的東西」，而且是 plan 制不按 token 計費 |
| **claude** | 審 diff、讀程式碼找原因、上網搜尋並判斷、寫給人看的字 | 它擅長「讀懂並判斷」 |

**codex 的 sandbox 預設禁外連**——要上網的任務不要給它，會卡在核准或直接失敗。
（生圖不受這條影響，見 §2.5。）

**挑之前先查額度。** `curl -s http://127.0.0.1:7717/v1/orchestrator/assistants`
（帶 `X-Clawdline-Orchestrator`）會回兩邊各自的 `availability`——`ok`、`low`、`exhausted`，
或近期沒人查過的 `unknown`。派給已經 `exhausted` 的那個會被 `409 assistant_exhausted` 擋下，
它的 `alternatives` 陣列會列出該改派誰。

### 2.5 產出是圖的時候

**codex 有真的影像模型。** 那不是退而求其次，也不需要 API key：`image_gen` 是它的內建工具、
預設開著，用的是 child 本來就登入的那個 Codex 帳號。一行可以查：

```bash
codex features list | grep image_generation      # → image_generation  stable  true
```

這個工具有兩個性質，直接決定指令要怎麼寫：

- **它不能被指定存到哪裡。** PNG 只會落在 `~/.codex/generated_images/<session-id>/*.png`，
  codex 自己的指引也明講不要依賴 destination 參數。**所以指令裡一定要寫：生完之後把檔案
  複製到 `/tmp/.clawdline/<id>/artifacts/`。** 漏掉這句，任務會以「圖存在但沒人拿得到、
  `artifacts/` 是空的」收場。
- **sandbox 擋不到它。** 畫圖發生在模型那一端，不是 child 自己連外，所以 §2.4 的禁外連
  跟這件事無關。2026-08-26 實測（codex-cli 0.149.1）：`codex exec -s workspace-write`，
  35 秒，約 14k tokens，產出 1254×1254 的 PNG。

**點陣還是向量是一個真的選擇，不是變通：**

| 要什麼 | 什麼時候 | 因為 |
|---|---|---|
| **`image_gen` 產的 PNG** | 插畫、材質、照片感的東西、hero 圖 | 那是一張畫，而且看起來就像一張畫 |
| **codex 手寫的 SVG** | 示意圖、icon、要能繼續改、要縮放、要能 diff 的東西 | 向量、檔案小，而且事後有人可以只改一條路徑 |

要透明背景就直接跟 `image_gen` 要、保留它的 alpha，不要因為「PNG 不能透明」就改寫 SVG。
另外還有一條需要 `OPENAI_API_KEY` 的 CLI 路徑（`gpt-image-2`、`gpt-image-1.5`）——child 沒有
理由用它，更不可以在內建工具就在手邊的時候悄悄降級過去。

### 2.6 哪個模型

`task.json` 的 `model` 欄位（選填，不寫就是那個助理的預設）。**只寫小寫字母、數字、`.`、`_`、`-`**，
其他字元 app 會回 `bad_task`。

| 模型 | 什麼時候 |
|---|---|
| `haiku` | 機械性、單一來源的活：抓一頁、抽三個事實、改格式。錯了一眼看得出來的那種 |
| `sonnet` | 一般有判斷成分的工作，葉節點的預設選擇 |
| `opus` | 有人會直接照著做的決定，以及**把好幾個 child 的答案合起來**的那個節點 |

**做審查的模型，不能比被審的東西所用的模型弱。** 用小模型去審大模型寫的東西，是花錢蓋橡皮圖章。

Codex 那邊同一個欄位填它的 slug（例如 `gpt-5.1-codex`）。

只有 Codex task 能選填 `reasoning_effort`，而且只能是 `high` 或 `xhigh`。寫程式建議用
`high`，規劃建議用 `xhigh`。不寫就沿用 Codex／模型與使用者的預設，command line 不會多出
override。空字串、非字串、任何其他名字（包括 `max`、`ultra`），以及搭配
`assistant: "claude"` 都會回 `bad_task`。手寫的 schedule template 可以帶這個欄位；排程編輯
雖然沒有 UI control，Codex 排程儲存時仍會保留它；若明確把助理改成 Claude，會移除這個不相容的
hidden override。

### 2.7 child 會不會卡在權限確認

**child 的分頁沒有人在看。** 一個停下來等你按「允許」的 session，會一路停到逾時——事後看起來
就是「這件事沒做」，而且沒有人知道為什麼。

`task.json` 的 `permission_mode` 有三個值（**沒有 `auto`**，見下面的警告）：

| 值 | 對到什麼 | 什麼時候用 |
|---|---|---|
| `ask` | 不帶 flag（＝ Claude Code 的 manual） | 只有你打算全程盯著那個分頁時 |
| `edits` | `--permission-mode acceptEdits` | 只讀檔寫檔、不跑指令的葉節點 |
| `full` | `--permission-mode bypassPermissions` | **預設**，也是實際上唯一跑得完的 |

**為什麼預設是 `full`。** 一個 dispatched session 的工作內容就是跑指令和寫檔案，比 `full` 窄的
每一格都會在某處停住：`ask` 停在它做的第一件事（讀自己的 CHILD.md），`edits` 過得了寫檔
但過不了 `cat`／`mkdir`／`curl`／`sleep`——而那些正是「往下派工」的全部內容。沒有任何 flag
蓋得住那些又停在 `full` 之前。這不放寬「誰能派工」（那還是 `0600` 的 token 檔）。

**⚠️ `auto` 是模型相依的，所以 Clawdline 不提供它。** 實測：`--permission-mode auto` 在
**Sonnet 與 Opus 上會得到 auto mode，在 Haiku 上會得到 `manual`**——每一步都問，比不帶 flag
還糟，而且沒有任何錯誤訊息。一個「在最便宜的模型上悄悄變成最嚴格」的值，不能放進派工的欄位。
更廣的教訓：**驗證 flag 的時候，模型也是一個變數。** 只用一個模型測會得到錯的結論。

**四道門，由內而外，前兩道 Clawdline 已經幫你處理好：**
1. 跨目錄**讀**（child 的任務在 `/tmp/.clawdline/` 而工作目錄不是）→ app 自動加 `--add-dir`
2. 跨目錄**寫**（寫自己的 result.json）→ `edits` 以上
3. **指令篩檢**——`jq -n` 加單引號 filter 會被判「大括號緊鄰引號 ＝ 混淆」，
   `... > f.tmp && mv` 會被判「無法靜態分析的 shell 語法」，**而且這兩個提示都沒有
   「總是允許」**。只有 `full` 過得去。所以**寫 JSON 用 heredoc，寫檔用 Write 工具**。
4. **信任目錄**——沒造訪過的目錄會先問 "Do you trust this folder?"，**任何 permission 設定
   都到不了**，任務會在兩分鐘後變成 `spawn_failed`。派工前那個目錄要有人手動開過一次。

**這台 Mac 的上限**在 Settings → Remote →「child 可以自己走多遠」（config.json 的
`orchestrator_permission`）。任務要得比上限多會被安靜降到上限，實際生效的值寫在 task record
的 `permission` 欄位裡，可以查。

**要驗證 child 有沒有被問過，只能即時看那個分頁的畫面。** 事後讀 transcript 看不出來——
被問然後被允許，跟從來沒問過，寫下來一模一樣；child 自己回報「沒被擋住」也不算數。

### 2.8 指令要自己站得住

child 讀不到這條對話，它只有 `task.json` 裡的 `instructions` 和 `plan`。「照剛剛講的做」在那裡
等於什麼都沒說。路徑寫絕對路徑，產出寫清楚要放哪個檔名。**葉節點的指令要窄到一句話講得完**——
要寫三段才講得清楚「做完」是什麼意思，那就是兩個 child。

**指令裡要求它在頭三分鐘發一則 `/progress`**，講它讀完 briefing 之後決定要怎麼做——在開工前，
不是做到一半。一行指令就換得到，child 只要花一個回合。

那一則是「能不能早點取消」的唯一依據，而差距不小：這台機器上最貴的兩筆被取消的 task，各燒了
18.5M 與 16.5M tokens、各跑了二十六分鐘，才有人看得出它走錯方向。協定原本只要求「工作內容不再
符合標題時」發一則——那個訊號是在偏離**之後**才到的，不是在偏離的當下。第一句話寫錯，第三分鐘
就看得出來，而那是這整套系統裡最便宜的一種更正。

---

## 3. 建目錄、生 id 和 secret

```bash
task_id=$(uuidgen | tr '[:upper:]' '[:lower:]')
secret=$(openssl rand -hex 32)
umask 077 && mkdir -p "/tmp/.clawdline/$task_id/artifacts" && chmod 700 /tmp/.clawdline "/tmp/.clawdline/$task_id"
echo "$task_id"
```

`umask` 和 `mkdir` 一定要在**同一個** bash 呼叫裡——每次工具呼叫都是新的 shell，分開寫 umask 就沒了。

`secret` 是 child 回報完成時的憑證，64 個 hex 字元。**它只走一條路**：由你交給 app（dispatch 的
body），app 再放進注入給 child 的第一句話。app 只留 SHA-256。
**不要把它寫進 `task.json`、不要寫進任何 `/tmp` 下的檔案。**

## 4. 寫 task.json

用 `jq -n` 組，不要手拼字串——`instructions` 裡有引號或換行時手拼一定會壞。

```bash
jq -n \
  --arg id "$task_id" \
  --arg kind "image" \
  --arg assistant "codex" \
  --arg dir "$PWD" \
  --arg title "專案肖像圖" \
  --arg instructions "……完整的任務說明……" \
  --arg created "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg root_session "$ROOT_SESSION" \
  --arg root_assistant "$ROOT_ASSISTANT" \
  --arg root_label "clawdline 主控 session" \
  --arg model "haiku" \
  --arg plan "$PLAN" \
  '{clawdline_protocol:1, task_id:$id, kind:$kind, assistant:$assistant, model:$model,
    permission_mode:"full",
    isolation:"none", project_dir:$dir, title:$title, instructions:$instructions, plan:$plan,
    deliverables:["artifacts/out.png"], timeout_minutes:30, created_at:$created,
    root:{session_id:(if $root_session=="" then null else $root_session end),
          assistant:$root_assistant, project_dir:$dir, label:$root_label}}' \
  > "/tmp/.clawdline/$task_id/task.json"
```

`$PLAN` 就是 §2.1 那張圖，**每一件都放同一份**——那是整張圖，不是這一件的說明。

欄位規則（違反就是 `422 bad_task`，app 不會幫你補）：

| 欄位 | 規則 |
|---|---|
| `clawdline_protocol` | 一定是 `1` |
| `task_id` | 小寫 UUID，**要跟目錄名、跟 dispatch body 裡的三者一致** |
| `kind` | `image` · `code-review` · `test` · `custom` |
| `assistant` | `claude` 或 `codex` |
| `project_dir` | 絕對路徑，而且那個目錄現在要存在 |
| `title` | ≤ 200 字，人看的一句話 |
| `instructions` | 非空、≤ 16 KiB |
| `deliverables` | 相對於 task 目錄的路徑，慣例是 `artifacts/…` |
| `claims` | 選填，但**還是要寫**：這件任務可能寫到的相對路徑，0…32 條、不重複、每條 1…1024 字元，開頭不能是 `/`、不能有 `..`。寫 `[]` 是正面宣告「這件事唯讀」。見下面那段 |
| `model` | 選填。小寫字母、數字、`.` `_` `-`，最多 64 字元。不寫 ＝ 該助理的預設模型 |
| `reasoning_effort` | 選填，只限 Codex。只能是 `high`（寫程式）或 `xhigh`（規劃）。不寫 ＝ 沿用 Codex／使用者預設且不加 CLI override；不接受 `max`、`ultra` |
| `permission_mode` | 選填。`ask`／`edits`／`full`。不寫 ＝ 這台 Mac 的上限值（預設 `full`）。寫別的字（包括 `auto`）＝ `bad_task` |
| `isolation` | 選填。`none`／`worktree`；不寫 ＝ `none`。只有通過 §2.0a 判斷才用 `worktree` |
| `isolation_base` | 選填 Git revision，只能跟 `isolation: "worktree"` 一起用；不寫就是實際開始時的 `HEAD` |
| `plan` | 選填但**強烈建議**：整張圖，≤ 4 KiB。同一批任務全部放同一份 |
| `timeout_minutes` | 1…240，沒寫當 30 |
| `root.session_id` | 目前這個助理的 conversation id（優先）或受監看的 terminal id；查不到就 `null`，不要瞎編 |
| `root.assistant` | **派出這件 task 的助理**，`claude` 或 `codex`；不是最外層 `assistant` 所指定的 child |
| `root.parent_task` | **只有你自己是 child 才要填**——填你自己那件 task 的 id（第一句話裡那個）。root 派工不用寫。填錯只會讓這件任務被算到別人頭上或被算得更深，不會佔到便宜 |

**宣告 `claims` 大約只花 root 二十個 output token，而多數派工還是沒寫。** 這台機器 206 筆派工
裡有 60.7% 什麼都沒宣告。撞一次的代價是整件 task 重來——同一份紀錄上是三百萬到一千八百萬 tokens。

還要知道 `claims` 救不了你的那一種情況：**worktree 隔離的 task，repository-relative 的 claims 會
被丟掉**，因為 child 改的是另一份 checkout。2026-08-28 兩個 root 相隔六秒派出同一件交付的修正，
兩件都是隔離的，沒有任何一道閘門攔得下來——`/inflight` 對兩邊都還是空的。**一旦用隔離，`/inflight`
就是唯一的檢查**，所以要去讀它，並且把預計會寫到的檔案留在 `plan` 裡，讓 review 還有範圍可循。

**`claims` 有兩種寫錯的方式，只有一種會叫。** 不寫是安靜的那種：broker 沒辦法證明你這件事跟別人
不相交，只好退回去對「每一對共用同一個目錄的任務」發警告。2026-08-26 那個晚上就是這樣——一晚十幾
條通知，沒有一條在講真的衝突；而那晚唯一一次真的撞上，是在派工當下就被
`409 workspace_busy` 擋掉的。**不寫 `claims` 不是保守，是拿一個答案去換一堆雜訊。**
宣告過寬是另一種，而它會自己叫出來：宣告過的路徑不管你有沒有碰，都在擋別人的樹；任務走到終局狀態
時，broker 會把「宣告了卻從沒碰過」的路徑一條條點名。同一個錯誤的反方向——收到那份報告就當真，
下一次宣告窄一點。

### 查自己的助理與 session id（best-effort，查不到就 null）

> `root.session_id` 優先填助理自己的 conversation id——Claude 的 transcript uuid 或 Codex 的
> rollout id；broker 現在也接受受監看的 terminal id。派工當下會把兩種拼法依
> `root.assistant` 解析成同一個 process-bound conversation key，完成通知、分組、容量與 root 關閉
> cascade 全部用這一把 key。一定要讀 response 的 `warnings`：非 null 的值若找不到唯一 live owner，
> 會回 `root_unresolved`；這種 Child 可能得自己 poll，也不能假設會隨 Root 關閉。


**Codex：**目前 rollout id 已直接放在環境變數裡。要跟寫 `task.json` 放在同一個 shell
呼叫，兩個變數才會一起進 `jq`：

```bash
ROOT_ASSISTANT=codex
ROOT_SESSION="${CODEX_THREAD_ID:-${CODEX_SESSION_ID:-}}"
echo "root session = ${ROOT_SESSION:-null} ($ROOT_ASSISTANT)"
```

`CODEX_THREAD_ID` 與相容名稱 `CODEX_SESSION_ID` 就是 Codex rollout 裡的
`session_meta.session_id`。Broker 只會在宣告的 Codex terminal **目前那個 pid** 正持有同一份
user rollout 時接受它；從舊 rollout 複製來的 id 不會把 child 掛到被重用的 terminal 下。

**Claude：**先設定助理，再用 nonce 去自己的 transcript 裡撈 id。

Claude Code 沒有辦法直接問「我是誰」，所以用一個 nonce 去自己的 transcript 裡撈。
**這招必須拆成兩個工具呼叫**——某個呼叫的指令文字，要等**那個呼叫結束之後**才會被寫進
transcript，所以「echo nonce 之後在同一個呼叫裡 grep」永遠撈不到（實測過，加 retry 也沒用）：

```bash
# 呼叫 A（跟步驟 3 同一個呼叫就行）：把 nonce 留進 transcript
ROOT_ASSISTANT=claude
echo "clawdline-nonce-$task_id"
```

```bash
# 呼叫 B（下一個工具呼叫，跟步驟 4、5 放同一個呼叫）：這時候才撈得到
ROOT_ASSISTANT=claude
slug=$(printf '%s' "$PWD" | sed 's/[^a-zA-Z0-9]/-/g')
f=$(grep -l "clawdline-nonce-<task_id>" "$HOME/.claude/projects/$slug/"*.jsonl 2>/dev/null | head -1)
ROOT_SESSION=$(if [ -n "$f" ]; then basename "$f" .jsonl; fi)
echo "root session = ${ROOT_SESSION:-null}"
```

原理：nonce 隨呼叫 A 的紀錄落進 transcript，呼叫 B 用 `grep -l` 找到的那個檔名（去掉 `.jsonl`）
就是本 session 的 id。slug 的規則是 `$PWD` 裡每一個非英數字元都換成 `-`。
`<task_id>` 記得代入真的 id——呼叫 B 是新的 shell，呼叫 A 的變數已經不在了。

**這一步要在主線做，不要丟給 subagent。** subagent 的 transcript 在
`~/.claude/projects/<slug>/<session-id>/subagents/agent-*.jsonl`，`*.jsonl` 這個 glob 抓不到它
（實測過），你會拿到空字串。真的在 subagent 裡跑到了，用這條補救——**那個檔案的上兩層目錄名
就是 session id**：

```bash
p=$(grep -rl "clawdline-nonce-$task_id" "$HOME/.claude/projects/$slug/" 2>/dev/null | head -1)
case "$p" in
  */subagents/*) ROOT_SESSION=$(basename "$(dirname "$(dirname "$p")")") ;;   # 上兩層＝session id
  *.jsonl)       ROOT_SESSION=$(basename "$p" .jsonl) ;;
esac
echo "root session = ${ROOT_SESSION:-null}"
```

呼叫 B 還是撈到空的，就再隔一個呼叫補撈一次；仍然沒有就填 `null` 往下走，不要卡在這裡。

**這件事有兩個用途，第二個很容易忘：** 一是完成時 app 要知道該回頭通知哪個終端機；二是
**清單裡那個 child 的 row 要縮排掛在你底下**，靠的就是這個 id。填 `null` 的話任務照樣跑，
但完成通知只能自己 poll，而且那一行會孤零零地浮在清單中間——上面標著 `Child`，卻不在任何人
底下，看起來像分組壞掉。查不到就填 `null`（不要瞎編），但查得到就一定要填。
`ROOT_SESSION` 與 `ROOT_ASSISTANT` 都要跟步驟 4 的 `jq` 在同一個 bash 呼叫裡，不然變數
不會留下來——或者直接把兩個字串貼進 `--arg`。

---

## 5. Dispatch

```bash
curl -s -X POST "http://127.0.0.1:$PORT/v1/orchestrator/tasks" \
  -H "X-Clawdline-Orchestrator: $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"task_id\":\"$task_id\",\"secret\":\"$secret\"}"
```

成功長這樣（`state` 會是 `queued` 或 `spawning`，分頁還沒開完）：

```json
{"ok":true,"task":{"id":"…","state":"spawning","kind":"image","title":"專案肖像圖",
 "assistant":"codex","projectDir":"/Users/you/code/clawdline","created":1787100000,
 "spawnedAt":1787100002,"dir":"/tmp/.clawdline/…","child":{"terminalId":"…","backend":"iterm"}}}
```

失敗一律是 `{"error":{"code":…,"message":…,"request_id":…}}`。**branch 在 `code` 上**：

| `code` | 意思 | 做什麼 |
|---|---|---|
| `depth_exceeded` | **你已經在樹的最底層** | 立刻停，照 §0 回報使用者，然後這件事自己做。不要繞路重試 |
| `over_capacity` | 額度滿了 | `message` 會說是你這個 session 的額度滿了、還是整台 Mac 的。錯誤物件裡有 `retry_after`（秒）。等它再送，或減件數／分批。**不要連續重打** |
| `bad_task` | `task.json` 不合格 | 讀 `message`，改檔案，同一個 `task_id` 再送一次（同 id 是冪等的）。`model` 打錯也走這條 |
| `forbidden` | token 錯或沒帶 | 重讀 token 檔；還是不行就是 app 重生過 token，請使用者重開 Clawdline |
| `rate_limited` | 10 分鐘內派超過 10 次 | 等窗口滾過去 |
| `not_found` | 路由不存在 | 這台的 Clawdline 沒有 orchestrator，請使用者更新 |

**同一個 `task_id` 重送是安全的**，回的是已登記的那筆，不會開第二個分頁。所以逾時重試照送即可，
不需要 `Idempotency-Key`。

---

## 6. 回報、等完成、關閉 root 的責任

派完馬上告訴使用者：**幾件、各是什麼、給誰做、產出會在哪**。一行一件，附 `task_id` 前 8 碼就夠了。

完成有兩條路，你不用選：

1. **被通知**——app 會往你的終端機打一行進來：
   ```
   [clawdline] task 3f9a21bc (專案肖像圖) finished: success — see /tmp/.clawdline/<id>/result.json
   ```
   看到這行就去讀 `result.json` 和 `artifacts/`，然後把結論講給使用者。
2. **自己 poll**——`root.session_id` 沒查到、或使用者現在就要知道：

```bash
curl -s "http://127.0.0.1:$PORT/v1/orchestrator/tasks/$task_id" \
  -H "X-Clawdline-Orchestrator: $TOKEN" | jq '.task | {state, summary, artifacts, usage}'
```

`state` 走 `queued → spawning → briefed → success | failure | timeout | cancelled | spawn_failed`。
**`briefed` 就是「child 正在做」**，那個狀態可以停很久，不是卡住。

**不要開一個 while loop 在那裡等。** 每隔一段時間、在使用者問起時查一次就好；child 動輒十幾分鐘，
把 root session 綁在輪詢上是最貴的用法。

要提早收掉：

```bash
curl -s -X POST "http://127.0.0.1:$PORT/v1/orchestrator/tasks/$task_id/cancel" \
  -H "X-Clawdline-Orchestrator: $TOKEN"
```

讀結果：

```bash
cat "/tmp/.clawdline/$task_id/result.json"
ls -la "/tmp/.clawdline/$task_id/artifacts/"
```

`result.json` 裡的 `summary` 是 child 自己寫的一句話，`artifacts` 是它宣稱的產出——
**宣稱歸宣稱，檔案在不在自己 `ls` 一次**。任務目錄在完成 24 小時後會被清掉，
使用者要留的東西要複製出來。

`symbols` 是事後補不回來的那一欄：child 這次修改引入的每一個名字——新的 function、type、欄位、
字串 key、它加的測試群組名稱。要的是名字，不是描述。這棵樹是共用的，等你要 commit 的時候，
那個 child 動過的檔案裡可能同時躺著兩三個 session 沒做完的東西，而這份詞彙就是你分辨「哪幾段是誰的」
的依據。用猜的已經產出過根本編譯不過的 staged tree。child 沒寫那一欄，不等於它宣告自己沒引入
任何名字——去問，或自己讀 diff 把名字抓出來，再開始 stage。

### Child 完成不等於程式碼完成

上面的 orchestrator state 描述的是 **child task**。產出程式碼的 graph 另外有一個 root-owned
obligation，語意如下：

```
delivered -> reviewed -> pending landing -> landed
```

- `success` 只表示 child 交出了它宣稱的東西。
- `SAFE TO LAND` 只表示獨立讀者沒找到 blocker；它把 root 的責任推到 **pending landing**，
  不代表已 merge、已 ship、已完成或 done。
- `landed` 才表示指定 target ref 已包含審過的修改，而且整合後的精確 tree 通過必要驗證。

除非另一個具名 root 接受了一份 Clawdline handoff，派工的 root 就是 landing owner。「未來某個人」
不是 owner。責任還在 `delivered`、`reviewed` 或 `pending landing` 時，不得對使用者回報完成。

### 回報 Root 自己完成的這一輪

Root 自己這一輪真的完成時——包含必要整合、驗證與 commit——把 authenticated session delivery
report 當成最後一個 tool action，再向使用者給 final answer。這是 Root 對應 Child `result.json` 的
路徑，但語意刻意較弱：只畫一個勾，表示**已交付，等待驗收**；絕不宣稱獨立 review 或 broker-verified
landing。

```bash
if [ -n "${TMUX_PANE:-}" ]; then
  ROOT_TERMINAL="$TMUX_PANE"
elif [[ "${ITERM_SESSION_ID:-}" == *:* ]]; then
  ROOT_TERMINAL="${ITERM_SESSION_ID##*:}"
else
  echo "無法解析這個 Root 的 terminal-neutral Clawdline id" >&2
  exit 1
fi
jq -n --arg summary "$SUMMARY" '{summary:$summary}' \
  | curl -sS -X POST \
      "http://127.0.0.1:$PORT/v1/orchestrator/sessions/$ROOT_TERMINAL/complete" \
      -H "X-Clawdline-Orchestrator: $TOKEN" \
      -H 'Content-Type: application/json' --data-binary @-
```

先把 `SUMMARY` 設成一句不超過 500 字、具體描述本輪交付的句子。只能在最後一輪仍是 working 時呼叫；
部分成果、只做診斷、遇到 blocker、正在問問題，或自己是 Child 時都不得呼叫。按 typed refusal 分支並如實
回報；自然語言不能代替缺少的 receipt。Clawdline 在同一個 terminal 開始下一個 observed turn 時會消耗
receipt，所以舊勾不會在新一輪未回報的工作後重新出現。

Root idle 且仍有 live Clawdline Child 時，broker 會投影成 `waiting_session`，卡片在安靜的 `⏳` 後列出
Child 工作；這是等待，不是待分流，也不是已交付。Root 同時做事仍顯示 `working`，Child 完成只會移除
等待 evidence，不會替 Root 的整合工作宣告完成。

有 claims 的 child 成果回來時，root 要用該 task secret 在原 task 上登記尚未關閉的義務：呼叫
`POST /v1/orchestrator/tasks/:id/landing`，body 是
`{"state":"pending","target":"<目標 ref>"}`。具名 root 接受 handoff 後，也可比照 cancel 與
claims release 使用機器層級的 orchestrator token。Child 不呼叫這條路由是協定慣例，不是機制隔離：
child 必然持有自己的 task secret；
`GET /v1/orchestrator/landings` 是大家都能查的告示牌，不會延長 claims 或擋住另一個 dispatch。

### 關閉一份 code delivery

終局 reviewer 給出 verdict 後，root 要完成下面每一步：

1. 讀 broker 的 worktree receipt 與 review evidence，確認 delivery branch、base、head、commit 數與
   clean/dirty 事實，正是 reviewer 審過的那一份。
2. 讀 target repository 當下的 branch、head 與 status；用 merge、cherry-pick 或 rebase 整合，
   但不得 stage、改寫或吸收另一個 session 原本就在工作樹裡的修改。共用樹裡，child 的 `symbols`
   清單就是你分辨「哪幾段是它的、哪幾段本來就在」的依據。
3. 重疊的 uncommitted work 讓整合不安全時，停止 merge 嘗試，但**責任維持 pending**。用 Clawdline
   的 session/task view 找到 owner、協調，等工作樹安全後再試。共享樹安全是等待的理由，不是把工作
   宣告完成的理由。
4. 依 repository 規則、用私有暫存路徑測整合後的精確 tree。你本來就在 stage，所以 index 才是該測的
   對象：測 `git write-tree` 出來的封存快照，不要測活的工作樹——那棵樹是所有人工作的聯集。
   接著確認 target ref 包含預期 delivery，並記下 target commit。
5. 用 task secret（或接受 handoff 後的機器 token），把 task 的 landing record 標成 `landed` 並
   附上已驗證的 commit。到這裡才能向使用者說 `landed` 或完成，並講出落到哪個 target 與 commit。

**HEAD 必須自己站得住，而唯一能把它弄壞的動作就是 commit。** 2026-08-26 這個 repository 裡發生了
兩次，來自兩個不同的 session：一次是整檔 `git add` 帶進三行，而定義它們型別的那個檔案還沒 commit；
一次是一條協定要求落地了，它的十四個值卻還留在工作樹裡。兩次 commit 的當下，樹都是綠的——陷阱就在
這裡：**只要還有東西沒 commit，樹是綠的就什麼都沒說**，因為那棵樹是所有人工作的聯集，而 HEAD 只有
你那一份。所以部分 commit 沒有在「它自己那一份能單獨編譯過」之前算完成；下手之前先問：我拿走的
東西，還有誰在定義它？宣告少了值、呼叫少了函式、case 少了 enum——這些在樹裡都會過，在 HEAD 裡都會炸。

### 透過 Clawdline 等檔案

共享樹等待不能用 Claude Code 原生 message、Codex 專屬 channel，或任何 assistant conversation id
協調。broker 邊界是 Clawdline：

1. 用本機 orchestrator credential 呼叫 `GET /v1/orchestrator/sessions` 找 owning root：它會列出
   每個 session 的 terminal-neutral `id`、`assistant`、`cwd`、`label`、`state`，Clawdline 自己開的
   分頁還會有 `taskId`。地址用那個 `id`，不用 provider-specific `sessionId`。
   **`GET /v1/sessions` 與 `GET /v1/sessions/:id/git` 是配對裝置的 API，對 orchestrator credential
   一律回 `401 unauthorized`**，所以 repository 的狀態改成在該 checkout 裡自己跑 `git` 讀。自己的 id
   是 `$ITERM_SESSION_ID` 冒號後面那段 UUID；`GET /v1/orchestrator/waits` 只認得已經在某個 wait 裡的
   id——兩者都構不到「還沒有人等過的 session」，那正是這條 index 存在的理由。
2. 用本機 orchestrator credential 呼叫 `POST /v1/orchestrator/waits` 登記 relationship，寫出
   `repository`、精確 `paths`、`owner_session_id`、`waiter_session_id`、`reason` 與
   `release_condition`。Clawdline 會 canonicalize paths、deduplicate waiter、持久保存 group，並把
   request 送進 owner 的 terminal。credential 只能拿來呼叫，不能印出或複製進 task。
3. commit 或明確釋放 paths 後，owner 用 `POST /v1/orchestrator/waits/:id/release`，附自己的
   session id 與有的話 commit。Clawdline 會 fan-out 給**每一個** waiter，逐筆記 delivery receipt；
   部分失敗時重試只送尚未收到的人。放棄工作的 waiter 只能 cancel 自己。
4. release notice 只是喚醒，不是證明。每個 waiter 在 stage 或 integrate 前都要重讀 target HEAD、
   status 與 diff。broker 永遠不能從某一次乾淨 status 推測 release。

未解 wait 會跨 app restart 保存，owner 消失時也仍看得到。`GET /v1/sessions` 把它公布給 app 自己的
UI，成為獨立的 `coordination` overlay：被擋住的 session 是 `waiting_on_session`／`waitingOn`，owner
是 `waitedOnBy`；拿 orchestrator credential 的 agent 讀 `GET /v1/orchestrator/waits` 得到同一組關係。
這**不會**把 terminal `state` 設成 `waiting`；後者仍只代表需要人回答，也只有它會觸發醒目的 row 與 push。原生與 web session row 會安靜顯示 `⏳ owner · release condition`；只有 UI
不可用時，才用最後一行 `[Clawdline waiting]` marker 當 fallback。

### 維持協定頁等於現在，並且分清楚你在寫給誰看

**文件依讀者分家，而分家決定它住在哪裡。** 一個專案的 `docs/` 是給社群的：英文、對外視角、進
git、可以讓測試依賴它。私有的工作文件——稽核、研究頁、用自己語言寫的計畫——放在那個專案存放內部
material 的地方，而**公開的測試不得依賴其中任何一個檔案**。把一份搬過去是重寫，不是複製。

在這個 repo 裡，living 的那一份是 `docs/clawdline-protocol.html`。任何 task dispatch、handoff、
claims、landing closure、file wait、ownership transfer、structured notice 或其他 cross-session
communication 語意變更，都要在協定工作關閉前更新它。它不是 dated audit，旁邊也不會有一份 dated
audit：它必須等於現在。

要讓 Claude Code session 讀完整 authoritative protocol，不能只轉述當下對話。它更新 diagrams、
state labels、source links 與 checklist；root 再檢查 standalone HTML，逐項對照 `AGENTS.md`、
`docs/dispatching.md`、`docs/landing.md`、`docs/orchestrator.md`、`docs/api.md`、`docs/handoff.md`、
這份 skill 與本機 dispatch policy。來源改了而那一頁沒改，landing obligation 就維持 pending。

這個 root 若必須在第 5 步之前停下來，要用 Clawdline handoff 交接，不能把責任丟著。handoff 的
`CURRENT STATE`、`OPEN THREADS`、`IMMEDIATE NEXT STEP` 必須寫出 delivery branch/base/head、target
branch 與目前 head、review verdict 與測試 receipts、重疊路徑及已知 owner，還有唯一一個下一步
landing action。handoff 本身不是 landing；Clawdline receipt 確認第一句已送達具名接收 root 前，
原 root 仍然負責。對這個被明確點名的 obligation，那張 receipt 是 ownership transfer 的時點，
不是程式碼已落地的證據。

---

## 7. Handoff——把這條線交給下一個 session

上面六步派出去的是一件**任務**。這一步交出去的是**整條線**：把這條對話知道的事寫成一份文件，
另一個 session 讀了它接著做。它開出來的那個 session 是新的 **root**，不是 child——沒有 secret、
沒有 timeout、沒有 `result.json`，而且你這個 session 關掉不會動到它。

**觸發詞：**「使用 Clawdline Handoff」「handoff 給新 session」「交接給下一個 session」
「明天用新 session 接著做」，以及 "use Clawdline Handoff"、"hand this over to a fresh session"、
"pick this up in Codex"、"continue this tomorrow in a new session"。

**`/compact` 就夠的時候不要用這個。** 同一個 harness、同一個目錄、只是普通的階段轉換——compact
就好。handoff 買的是**可攜性、不是壓縮**：要用在工作必須**移動**的時候（換階段、換 harness 或
模型、明天再繼續、平行分岔、換機器），或是 context 快滿到會替你做決定的時候。整條線比那份文件
還小的，也不要交接。

**你自己是 child（§0）的話，這一步也不是你的。** 把一條線交出去是 root 對自己那條對話的決定，
child 開一個新的 root 等於走出了它被放進去的那棵樹。回報給你的任務，讓 root 決定。

四步。

**1. 照八個標題寫文件。** `OBJECTIVE`／`KEY DECISIONS`（標「不要重新開題」，每條附日期）／
`CURRENT STATE`／`REFERENCES`／`CONSTRAINTS & PRINCIPLES`／`OPEN THREADS`（編號）／
`IMMEDIATE NEXT STEP`（一個入口）／`VERIFICATION`。

兩條規則撐起大半。**引用過的東西不要重複寫一遍內容**——指路就好；一份會摘要自己來源的交接文件，
禮拜四就會跟來源打架。還有 **`VERIFICATION` 是三到五題「答案刻意不在這份文件裡」的題目**，
每題點名答案在哪：這才是逼接收端真的走完引用鏈的東西，而答不出來的那一題，就是在最便宜的時刻
被抓到的斷點。交出去前把指路自己校一次——指錯的路和斷掉的鏈，看起來一模一樣。

**2. 引用到易失的東西，先固化再引。** 活在 session scratchpad 裡的設計文件、只有 URL 的
artifact、`/tmp` 底下的檔案：先複製進 repo——留紀錄用 `artifacts/`、要當常設答案用 `docs/`——
然後引用那份固化的副本。引用不重複，但易失來源是例外。這步是最多人跳過的，也是一週後鏈會斷在
那裡的那一步。

**3. 建手交包。**

```bash
hid=$(uuidgen | tr '[:upper:]' '[:lower:]')
umask 077 && mkdir -p "/tmp/.clawdline/handoffs/$hid" && chmod 700 /tmp/.clawdline/handoffs "/tmp/.clawdline/handoffs/$hid"
echo "$hid"
```

用你的寫檔工具把 `handoff.md` 寫進那個目錄，接收端該讀的東西放旁邊的 `attachments/`——
而且**每個附件都要在 `REFERENCES` 裡點名**，用相對於 `handoff.md` 的路徑。沒有被引用點名的附件，
接收端永遠不會打開：它拿到的那一句只指向 `handoff.md`，不指向別的東西。
沒有 secret、沒有 token、沒有別的——handoff 裡沒有憑證。

**4. 開 session。**

```bash
curl -s -X POST "http://127.0.0.1:$PORT/v1/orchestrator/handoffs" \
  -H "X-Clawdline-Orchestrator: $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"handoff_id\":\"$hid\",\"project_dir\":\"$PWD\",\"title\":\"Cloud 規劃線\",\"from_session\":\"$ROOT_SESSION\"}"
```

`assistant`（`claude`／`codex`，不給就是 `claude`）與 `model` 都是選配。`title` 是分頁的名字——
不給的話分頁就叫 `handoff` 加 id 前八碼；`from_session` 是收據要打去哪：填你自己這個 session 的
id，200 字元以內，認不出來就等於沒填。兩個都是 best-effort，因為 **app 不會為了湊出這兩個值去開
`handoff.md`**。`code` 照 §5 的方式 branch：`forbidden`、`orchestrator_disabled`（Settings 裡那個
開關連 handoff 一起管）、`bad_request`、`bad_task`（欄位不對，或手交包目錄、`handoff.md` 根本
不在）、`rate_limited`（跟 dispatch 共用同一個煞車，被擋掉也照吃一格）、`not_found`（這版沒有
handoff 路由）。

遇到 `not_found` 時，做完第 1–3 步，並把
[`docs/handoff.md`〈The line〉](../../docs/handoff.md#the-line) 的 canonical 句原封不動交給使用者自行貼上：
`You are picking up a Clawdline handoff. Read /tmp/.clawdline/handoffs/<id>/handoff.md before anything else and follow it: walk its REFERENCES, answer its VERIFICATION questions from those sources, say plainly what you could not reach, then continue from OPEN THREADS.`

然後用兩行告訴使用者東西去哪了——這次交接涵蓋什麼、檔案在哪。如果你自己還要繼續做而不是就此
停手，講出來：**handoff 是複製，不是結束。** 一份工作區裡兩個 root，代表你們兩邊之後每次派工
都要宣告 `claims`，而這棵樹自己的規矩（[`AGENTS.md`](../../AGENTS.md)）會在新 session 進門時
自己送到它面前。

完整協定——手交包的長相、app 為什麼從不讀那份文件、接收端欠什麼、路由的驗證與錯誤碼——在
[`docs/handoff.md`](../../docs/handoff.md)。

---

## 8. Schedule——讓 task 模板到點派工

只有使用者要 Clawdline 自己週期派工時才用。把一份嚴格 JSON 寫到
`~/.config/clawdline/schedules/<小寫-uuid>.json`；從
[`docs/schedules.md`](../../docs/schedules.md) 的完整 schema 開始，不要自創欄位。
`when.at` 是這台 Mac 的本地 `HH:MM`，`days` 是 `daily` 或 weekday 名稱。
`close_tab` 要明講選擇：`on_success` 會把失敗 tab 留給人接手，`always` 任何結果都收，
`never` 不做排程專屬的立即關閉，沿用現有 orchestrator linger。

寫完先用 read route 驗格式：要找到它的 `id`；若看到 `state` 是 `invalid`，先停下來回報該列的
`error`。只有格式有效才手動跑一次：

```bash
curl -s "http://127.0.0.1:$PORT/v1/orchestrator/schedules" \
  -H "X-Clawdline-Orchestrator: $TOKEN"
curl -s -X POST \
  "http://127.0.0.1:$PORT/v1/orchestrator/schedules/<schedule-id>/run" \
  -H "X-Clawdline-Orchestrator: $TOKEN"
```

要讀 dispatch 回應與 task record；不能只因為檔案存在就說安裝驗過。也要把誠實界線告訴使用者：
app 沒開就不會觸發，重開後也只在 `catch_up_hours` 內補課。

如果一件任務有用的產出**就是一則及時訊息**——例如每日天氣預報——要在 instructions 裡明講：
照 `CHILD.md` 提供的 task-secret `/notify` 路由推播。例行結果仍寫進 `result.json`；推播只留給
使用者正在等的罕見答案。本機 root 腳本則可帶 orchestrator token，`POST
/v1/orchestrator/notify`，body 是 `{"title":"…","body":"…"}`。title 最多 80 字元、body
最多 500；每個 task 5 則，整台 Mac 每小時 30 則。task secret 可在任務存活期間，以及完成後僅
60 秒內推播；通知型任務在可行時要先送出內容，再寫最後的 `result.json`。
使用者可以在「設定 → 遠端」關掉 agent 通知。收到 `409 agent_notify_disabled` 代表內容沒有
送達、額度也沒有消耗：不要重試，把內容留在 `result.json`，如實回報這次拒絕；若是排程任務，
任務本身仍照常完成。

---

## 一個完整的例子——叫 codex 畫一張這個專案的肖像

使用者說：「幫我叫 codex 畫一張這個 project 的樣子，中世紀手繪風格。」

**呼叫 A**——確認環境、建目錄、留 nonce（secret 這裡還不需要）：

```bash
PORT=$(jq -r '.remote_port // 7717' ~/.config/clawdline/config.json 2>/dev/null || echo 7717)
TOKEN=$(cat ~/.config/clawdline/orchestrator-token)
task_id=$(uuidgen | tr '[:upper:]' '[:lower:]')
umask 077 && mkdir -p "/tmp/.clawdline/$task_id/artifacts" && chmod 700 /tmp/.clawdline "/tmp/.clawdline/$task_id"
echo "clawdline-nonce-$task_id"
echo "task_id=$task_id"
```

**呼叫 B**——撈 session id、生 secret、寫 task.json、dispatch（`task_id` 代入呼叫 A 印出的那個）：

```bash
PORT=$(jq -r '.remote_port // 7717' ~/.config/clawdline/config.json 2>/dev/null || echo 7717)
TOKEN=$(cat ~/.config/clawdline/orchestrator-token)
task_id=<呼叫A印出的id>
secret=$(openssl rand -hex 32)

if [ -n "${CODEX_THREAD_ID:-${CODEX_SESSION_ID:-}}" ]; then
  ROOT_ASSISTANT=codex
  ROOT_SESSION="${CODEX_THREAD_ID:-${CODEX_SESSION_ID:-}}"
else
  ROOT_ASSISTANT=claude
  slug=$(printf '%s' "$PWD" | sed 's/[^a-zA-Z0-9]/-/g')
  f=$(grep -l "clawdline-nonce-$task_id" "$HOME/.claude/projects/$slug/"*.jsonl 2>/dev/null | head -1)
  ROOT_SESSION=$(if [ -n "$f" ]; then basename "$f" .jsonl; fi)
fi

jq -n --arg id "$task_id" --arg dir "$PWD" --arg created "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      --arg rs "$ROOT_SESSION" --arg ra "$ROOT_ASSISTANT" \
  '{clawdline_protocol:1, task_id:$id, kind:"image", assistant:"codex",
    project_dir:$dir, title:"專案肖像：中世紀手繪風",
    instructions:"你在 /Users/you/code/clawdline，這是一個 macOS 選單列 app，會盯著終端機裡的 Claude Code 與 Codex session，把它們的狀態畫在選單列、瀏海和一個浮動面板上。請先讀 README.md 和 docs/interface.md 弄懂它在做什麼，然後畫一張代表這個專案的圖：中世紀手抄本插畫風格，要有裝飾性邊框與手繪筆觸，高度藝術性。請用你的內建 image_gen 工具，橫幅、高品質。image_gen 只會寫到 ~/.codex/generated_images/<session>/、不能指定存檔位置，所以生完之後把那個 PNG 複製到 /tmp/.clawdline/<TASK_ID>/artifacts/project-portrait.png，並用 ls -la 確認檔案在。完成後照 CHILD.md 寫 result.json。",
    deliverables:["artifacts/project-portrait.png"], timeout_minutes:30, created_at:$created,
    root:{session_id:(if $rs=="" then null else $rs end), assistant:$ra,
          project_dir:$dir, label:"clawdline 主控"}}' \
  > "/tmp/.clawdline/$task_id/task.json"

curl -s -X POST "http://127.0.0.1:$PORT/v1/orchestrator/tasks" \
  -H "X-Clawdline-Orchestrator: $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"task_id\":\"$task_id\",\"secret\":\"$secret\"}"
```

（`instructions` 裡的 `<TASK_ID>` 記得換成真的 id——child 只會照字面讀。
實測：這樣拆兩個呼叫，session id 一次就撈到；擠在同一個呼叫裡則一定是空的。）

然後對使用者說：

> 派出去了：**專案肖像：中世紀手繪風**（`3f9a21bc`，codex，30 分鐘上限）。
> 它會用 codex 的內建影像模型畫，然後把 PNG 複製到
> `/tmp/.clawdline/3f9a21bc-…/artifacts/project-portrait.png`。
> 它做完會有一行通知進來，我再幫你看。

要的如果是示意圖或 icon 而不是插畫，同一件任務就改成請它手寫 SVG——見 §2.5。

---

## 幾條容易踩的

- **secret 只在 dispatch 的 body 裡出現一次。** 它會留在你這條 transcript 裡（0600，跟 token 檔同一個
  信任邊界），但**不可以**進 `task.json`、`CHILD.md` 或任何 child 讀得到的地方。
- **`/tmp/.clawdline` 底下不是你的公共空間。** 只碰自己這一件的目錄，不要去讀別人的 task。
- **一件事一個 child。** 「順便把測試也跑一下」寫進同一個 `instructions`，等於一個 child 兩個目標，
  失敗時你分不出是哪一半壞了。
- **葉節點跑完不等於圖跑完。** 如果圖裡有一個「彙整」節點，那個節點要等所有葉節點的
  `result.json` 都在了才派得出去——它的 `instructions` 要指名去讀哪幾個
  `/tmp/.clawdline/<id>/artifacts/`。這是 root 的責任，app 不會替你排順序。
- **reviewer 跑完不等於程式碼完成。** `SAFE TO LAND` 會建立一個 root-owned pending landing
  obligation。要嘛在具名 target 上關掉它並驗證整合後 tree，要嘛交給一個具名 root；不能單靠 verdict
  把它翻成「做完了」。
- **hunk-review 過的 index 要用無 pathspec 的 commit。** `git add -p` 與 staged-tree 驗證後，使用
  純 `git commit -m <message>`。`git commit -- <path>...` 會從 worktree 取出具名檔案，可能把 reviewed
  index 已排除的外來 unstaged hunks 一起吸進 commit。只有每個具名檔案的全部 worktree hunks 都屬於
  同一批 delivery 時，pathspec commit 才安全。
- **file waiter 是 Clawdline relationship。** request 與 release 都用 Clawdline session id，paths
  釋放時通知每一個 waiter，收到後再重驗。這份安全協定不能依賴 Claude Code 或 Codex message。
- **協定頁是協定的一部分。** 任何 communication semantics 修改，都要更新這個 repo 的公開協定頁
  （這裡是 `docs/clawdline-protocol.html`），並在完成前對照所有 authoritative source 驗證。私有的
  工作文件不能替代它：能被任何人檢查的是那份進了 git 的英文頁。
- **工作樹裡多出來的東西，先假設不是 child 做的。** 這台 Mac 上通常有好幾個 session 共用同一個
  工作區，而它們也在改檔案、也在 commit。派工之後看到 `git status` 多了幾個檔、或 `git log`
  多了一筆，**那不是 child 的成績單**——在把任何改動算到 child 頭上之前，先做這三件事：

  ```bash
  git log --format='%h %ad %s' --date=format:'%H:%M' -5   # 時間對得上你派工的時間嗎？
  git diff --stat                                          # 哪些檔案動了
  git diff -- <某個檔> | grep '^+' | head -20              # 改的主題是你派的那件事嗎？
  ```

  **看內容，不要看檔名。** 判準是「這段改動在講的事情，是不是我給它的任務」。一個你派去做
  A 功能的 child，不會順手寫出一個 B 功能——看到 B，那幾乎一定是別人的。

  這件事錯得起的代價很高：把別人的工作算成 child 的，你會做出錯誤的 review 結論、可能
  `git checkout` 掉同事跑了半小時的東西、還會對這個 child 的能力形成完全錯誤的印象。
  反過來也一樣——child 改到一半的東西可能被另一個 session 順手 commit 進去。
- **child 不 commit，root 才 commit。** 指令裡要明講禁止 `git commit`／`stash`／`reset`／
  `checkout`。它們在共用工作樹裡執行，一個 `git reset --hard` 會連別人的東西一起帶走。
- **child 跟 root 問的是不同的問題，所以驗證方式也不同。** child 問的是「我寫的東西會不會動」——
  該測的就是工作樹，連別人做到一半的東西都算在內，因為 child 不 commit，那些東西不會透過它進到
  HEAD。child 要跑測試的話，把這段照抄進 `instructions`：

  ```bash
  snapshot_dir=$(mktemp -d); test_tmp=$(mktemp -d)
  git archive "$(git stash create)" | tar -x -C "$snapshot_dir"
  (cd "$snapshot_dir" && TMPDIR="$test_tmp" ./test.sh)
  ```

  `git stash create` 是上面那條禁令唯一的例外：它只寫出一個裝著工作樹的 commit object，
  既不動工作樹也不動 stash 清單。樹乾淨的時候它什麼都不印，那就退回用 `HEAD`。
  **絕對不要叫 child 用 `git write-tree`**——那讀的是 *index*，所以得先 stage，而 index 是共用的：
  child 會把別的 session 留在裡面的東西一起掃進去，然後被某個 root commit 出去。這件事在這裡發生過。
  `write-tree` 是 root 的工具，因為 root 本來就在 stage，而「commit 下去 HEAD 還編得過嗎」
  這個問題只有 index 答得出來。
- **派工的規矩是使用者的，不是你的。** `~/.config/clawdline/dispatch-policy.md` 跟你的判斷牴觸時
  照它做，並在回報時說一聲你照了哪一條。
- **child 開的是真的終端機分頁，跑的是真的指令。** 派出去等於授權它在那個 `project_dir` 動手。
  會改到重要東西的任務，先跟使用者確認一次。
- **`project_dir` 要是這台 Mac 已經信任過的目錄。** Claude Code 第一次在某個資料夾啟動時會
  先問「Do you trust this folder?」——那不是權限提示，是啟動前的一道門，`permission_mode`
  管不到它。child 會停在那個畫面上，app 注入不進去，兩分鐘後變成 `spawn_failed`，而畫面上
  只有一個看起來莫名其妙的選單。要派到新目錄，先請使用者在那裡手動開一次 claude。
- **你自己是 child 又往下派的話，等它做完再寫自己的 `result.json`。** 你這件事一結束，
  你派出去的那些會一起被收掉——你的完成就是它們的死線。
- 完整協定（狀態機、file 格式、API、成本計算）在
  [`docs/orchestrator.md`](../../docs/orchestrator.md)。
