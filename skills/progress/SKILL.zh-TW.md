---
name: progress
description: |
  在**開始跑**或**動手寫**任何會超過大約半分鐘的東西之前使用——build、測試 suite、lint、資料匯入、
  migration、批次作業、deploy、影片轉檔，或一支專門用來跑這些的腳本——讓按下去的那個人可以看著它
  跑到哪，而不是只能猜。包住整條命令只要一行：
  `clawdline-progress run --label lint --typical 120 -- ./scripts/lint.sh`，這一輪就會出現在
  Clawdline 的 bar 和終端機 status line 上，帶著已經跑多久，以及在有人量過的情況下跑到幾成。
  觸發語句包括「跑一下測試」「開始編譯」「跑一下匯入」「發佈上去」「這要跑很久」「還在跑嗎」
  「大概還要多久」，以及等義的 "run the suite"、"build it"、"how much longer"、"is it still
  running"。往腳本、Makefile 或 CI 裡加一個慢步驟時也適用。一下就結束的命令不要用；只是要讀或說明
  狀態檔也不要用——這個 skill 是**產生**進度的那一端，不是讀它的那一端。
---

# 給正在等的人看的進度

**讀的人拿到什麼。** 一條跑很久的命令在結束以前通常什麼都不說，於是按下去的人要嘛守在那個終端機
前面，要嘛完全不知道現在怎麼樣。這支 helper 會在跑的過程中寫一個小檔案，Clawdline 的 bar 和
claude-bestiary 的終端機 status line 會把它畫出來：正在跑什麼、在哪一棵樹、從什麼時候開始、目前
在哪個階段，以及——在有測量值的時候——跑到幾成。**用手機看的人不必再猜還要多久。**

它跟測試或 build 沒有綁定。`label` 和 `phase` 都是自由文字，所以 lint、資料匯入、migration、
影片轉檔用的是同一份紀錄。

## 那一行

```sh
clawdline-progress run --label lint --typical 120 -- ./scripts/lint.sh
```

**優先用這個型式。** helper 就是那個 parent process，所以 exit status 是它自己的、signal 也是它
自己的：它用命令自己的 status 離開，被 kill 掉的一輪就會被記成被 kill 掉。你不用裝任何 trap，也就
不可能把 trap 寫錯。

- `--label` 是人看的字。兩三個字，講正在做的事：`lint`、`import`、`staging deploy`。不給的話會用
  命令自己的檔名。
- `--typical <秒>` 是**選填的，而且只填真的有人量過的時間**。有它，bar 才會從「已經跑多久」變成
  「跑到幾成」。沒量過就不要給：欄位會是缺席而不是被編出來的——這正是重點，對讀的人來說，編出來的
  數字和量出來的數字長得一模一樣。這個 repository 的 `./build.sh` 就是因為這樣一個都不給。
- `--stale-after <秒>`（預設 900）是讀的人該從什麼時候開始不再相信一列已經沒人在寫的 `running`。
  `--log <路徑>` 指出值得打開來看的 log。

檔案在哪，依序找：有設 `$CLAWDLINE_PROGRESS` 就用它；否則
`/Applications/Clawdline.app/Contents/Resources/clawdline-progress.sh`；否則 `$HOME/Applications`
底下的同一條路徑；否則在這個 repository 的 checkout 裡是 `Resources/clawdline-progress.sh`。

## 在一支有自己階段的腳本裡

```sh
. "$CLAWDLINE_PROGRESS"
progress_start --label test --typical 288
progress_phase guards
progress_phase compiling
# 不用寫收尾：progress_start 裝好的 trap 會從 exit status 決定
```

`progress_start` 會裝上 ERR、INT、TERM **和** EXIT。`progress_phase <字>` 換掉 bar 上顯示的階段
——那些字是逐字畫出來的，所以要寫給人看。`progress_clear` 把那一列整個拿掉；沒有「finish」要呼叫。

**如果這支腳本本來就有自己的 EXIT handler**，bash 只保留一個，第二個 `trap … EXIT` 會無聲地蓋掉
helper 那個。要用組合的寫法：

```sh
cleanup() {
  local status=$?
  # …這支腳本本來就要收的東西…
  if declare -F clawdline_run_file_exit >/dev/null 2>&1; then clawdline_run_file_exit "$status" || true; fi
  return "$status"
}
trap cleanup EXIT
```

## 為什麼這是一支 helper，不是四行說明文件

那份紀錄本身簡單到可以自己手刻，它外面那圈 trap 不是。兩個拿到完整說明的 agent 各自把它寫錯，在
同一台機器上，兩個都是量出來才走出來的：

- **只裝 `EXIT` 會把被 kill 的一輪讀成乾淨結束**，因為那個 handler 裡的 `$?` 是 0。
- **`set -e` 不會為函式裡面的失敗觸發 `ERR`**，而且任何 ERR trap 都看不到刻意的 `exit 1`。
- **handler 用 return 而不是 exit**，會讓被 `TERM` 的腳本從中斷的地方繼續跑完，然後宣告成功。

三個的結局是同一個：這一輪明明沒成功，bar 上卻說成功了。`Tests/progress-helper.mjs` 會把 helper
複製一份、把那一行拿掉，再把每一種失敗真的跑出來，所以「有修」這件事是被證明過的，不是被宣稱的。

## 它不會做的事

它絕不會把自己正在回報的那一輪弄壞：沒有 `$HOME`、cache 目錄寫不進去、磁碟滿了，代價就是那個 bar，
沒有別的。它只寫一個檔案，用工作目錄當 key 而且**一個字都不截斷**，先寫暫時檔名再 rename，所以讀
的人看到的要嘛是完整的新檔、要嘛是完整的舊檔，不會是各一半。它不 poll、不啟動任何東西、也不需要
app 有在跑——那個檔案是誰剛好去看誰就讀得到。
