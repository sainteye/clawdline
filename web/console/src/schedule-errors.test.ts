import assert from "node:assert/strict"
import test from "node:test"

// @ts-expect-error -- a `.ts` path for node's type-stripping test runner.
import { invalidScheduleErrorHTML, scheduleErrorCopy } from "./schedule-errors.ts"
// @ts-expect-error -- a `.ts` path for node's type-stripping test runner.
import { nextWord } from "./next-strings.ts"

function inLanguage<T>(language: string, run: () => T): T {
  const before = Object.getOwnPropertyDescriptor(globalThis, "document")
  Object.defineProperty(globalThis, "document", {
    configurable: true,
    value: { documentElement: { lang: language } },
  })
  try {
    return run()
  } finally {
    if (before) Object.defineProperty(globalThis, "document", before)
    else Reflect.deleteProperty(globalThis, "document")
  }
}

test("an invalid schedule says the classified problem and keeps parser prose in technical details", () => {
  inLanguage("zh-Hant", () => {
    const producer = "invalid character '}' looking for beginning of object key string"
    const html = invalidScheduleErrorHTML({ error_kind: "unreadable_json", error: producer }, nextWord)
    const encodedProducer = "invalid character &#39;}&#39; looking for beginning of object key string"

    assert.match(html, /這個排程檔不是可讀取的 JSON 物件，已停用。/)
    assert.match(html, /<summary>技術細節<\/summary>/)
    assert.match(html, new RegExp(`<code>${encodedProducer}</code>`))
    assert.equal(html.indexOf(encodedProducer) > html.indexOf("<details"), true)
  })
})

test("every producer classification has its own sentence and unknown kinds stay generic", () => {
  inLanguage("zh-Hant", () => {
    assert.equal(scheduleErrorCopy({ error_kind: "schema" }, nextWord).sentence, "這個排程檔的內容不符合格式，已停用。")
    assert.equal(
      scheduleErrorCopy({ error_kind: "project_unavailable" }, nextWord).sentence,
      "這個排程指定的專案資料夾目前無法使用，已停用。",
    )
    assert.equal(scheduleErrorCopy({ error_kind: "new_kind" }, nextWord).sentence, "無法讀取這個排程檔，已停用。")
  })
})

test("technical details escape producer markup", () => {
  inLanguage("zh-Hant", () => {
    const html = invalidScheduleErrorHTML({ error_kind: "schema", error: `<script>alert("x")</script>` }, nextWord)
    assert.doesNotMatch(html, /<script>/)
    assert.match(html, /&lt;script&gt;alert\(&quot;x&quot;\)&lt;\/script&gt;/)
  })
})
