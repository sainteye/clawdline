import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node's strip-types runner.
import { batchReadingWords, retainedStateWords, scanFailureWords, totalSessionWords } from "./session-reading.ts"

const source = { observed_at: 1_000, provenance: "iterm", freshness: "unverified" as const }

test("a recent retained reading is still the current-looking row, without an age note", () => {
  assert.equal(retainedStateWords({ source, state: "working" }, 1_005, true), null)
  assert.equal(retainedStateWords({ source, state: "working" }, 1_020, true), null)
})

test("an age note starts after three normal ten-second list refreshes", () => {
  assert.equal(retainedStateWords({ source, state: "working" }, 1_029, true), null)
  assert.equal(retainedStateWords({ source, state: "working" }, 1_030, true), "30 秒前在跑")
})

test("an older retained row says what was observed without duplicating unknown", () => {
  assert.equal(retainedStateWords({ source, state: "working" }, 1_090, true), "1 分鐘前在跑")
  assert.equal(retainedStateWords({ source, state: "waiting" }, 1_090, true), "1 分鐘前在等你回答")
  assert.equal(retainedStateWords({ source, state: "idle" }, 1_090, true), "1 分鐘前沒有新輸出")
  assert.equal(retainedStateWords({ source, state: "unknown" }, 1_090, true), "1 分鐘前讀到")
  assert.equal(retainedStateWords({ source: { ...source, freshness: "current" }, state: "working" }, 1_120, true), null)
})

test("the batch owns the failure sentence", () => {
  assert.equal(batchReadingWords(source, 1_120, true), "這次沒有讀完整；其中保留的內容最後在 2 分鐘前讀到。")
  assert.equal(
    batchReadingWords({ observed_at: 0, provenance: "iterm", freshness: "missing" }, 1_120, true),
    "這次沒有讀完整；至少一個來源沒有仍可採用的上次讀數。",
  )
})

test("a whole terminal-source failure says the next action instead of waiting forever", () => {
  assert.equal(
    scanFailureWords(["iTerm2 apple event failed: exit status 1"], [{ source: "iterm", complete: false }], true),
    "Clawdline 讀不到 iTerm2。請到「系統設定 → 隱私權與安全性 → 自動化」，允許 Clawdline 控制 iTerm2。",
  )
  assert.equal(
    scanFailureWords(
      ["tmux is at /opt/homebrew/bin/tmux, which is not on this daemon's PATH, and the tmux backend runs `tmux` from the PATH"],
      [{ source: "tmux", complete: false }],
      true,
    ),
    "Clawdline 在 /opt/homebrew/bin/tmux 找到 tmux，但 daemon 的 PATH 沒有它；這次無法確認 tmux 的 session 清單。",
  )
})

test("the title's number is the same set as the list", () => {
  assert.equal(totalSessionWords(6, true), "6 個 session")
  assert.equal(totalSessionWords(1, false), "1 session")
})
