import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

// Load the shipped `.js` as an ES module without depending on a repository-wide package type.
// Browsers already know it is a module from its import edge; this data URL gives Node the same
// fact and keeps the test runnable in a clean archive by itself.
const source = await readFile(
    new URL("../Resources/web/app/js/view/user-messages-data.js", import.meta.url),
    "utf8"
);
const styles = await readFile(
    new URL("../Resources/web/app/css/user-messages.css", import.meta.url),
    "utf8"
);
const data = await import("data:text/javascript;base64," + Buffer.from(source).toString("base64"));
const { copyForUserMessages, filterUserMessages, userMessageEntries, userMessagePosition } = data;

const transcript = [
    { role: "assistant", text: "long answer" },
    { role: "user", text: "first question", at: 10 },
    { role: "tool", text: "rg" },
    null,
    { role: "user", text: "second question", at: 20 }
];
const pending = [
    { role: "user", text: "still sending", pending: true },
    { role: "assistant", text: "not shown" }
];

const messages = userMessageEntries(transcript, pending);
assert.deepEqual(messages.map((entry) => entry.text), [
    "still sending",
    "second question",
    "first question"
]);
assert.equal(messages[0], pending[0], "the newest pending turn is first");
assert.equal(messages[2], transcript[1], "entries are not copied or rewritten");
assert.deepEqual(userMessageEntries(null, undefined), []);

assert.deepEqual(filterUserMessages(messages, "SECOND").map((entry) => entry.text), [
    "second question"
], "search is case-insensitive");
assert.deepEqual(filterUserMessages(messages, "  still sending  ").map((entry) => entry.text), [
    "still sending"
], "surrounding whitespace does not change the search");
assert.equal(filterUserMessages(messages, "missing words").length, 0);
assert.equal(filterUserMessages(messages, "").length, messages.length,
    "an empty search preserves every entry and its order");

assert.equal(userMessagePosition(transcript, pending, pending[0], false), 2,
    "a pending message maps to its row in an oldest-first transcript");
assert.equal(userMessagePosition(transcript, pending, transcript[4], false), 1,
    "a saved message maps by identity, not by its text");
assert.equal(userMessagePosition(transcript, pending, transcript[1], true), 2,
    "the target follows the transcript's newest-first order");
assert.equal(userMessagePosition(transcript, pending, { role: "user", text: "second question", at: 20 }, false), -1,
    "an equal-looking copy cannot jump to the wrong turn");

assert.equal(copyForUserMessages("zh-TW").title, "我傳出的訊息");
assert.equal(copyForUserMessages("zh-Hant").empty, "你還沒有在這個 session 傳出訊息。");
assert.equal(copyForUserMessages("zh-TW").search, "搜尋我講過的話");
assert.equal(copyForUserMessages("zh-TW").noMatches, "找不到符合的訊息。");
assert.equal(copyForUserMessages("zh-CN").title, "我发出的消息");
assert.equal(copyForUserMessages("zh-CN").search, "搜索我说过的话");
assert.equal(copyForUserMessages("en-US").title, "My messages");
assert.equal(copyForUserMessages("en-US").search, "Search what I said");
assert.equal(copyForUserMessages("ja-JP").title, "My messages", "unknown locales use English");

assert.match(styles, /\.user-message-list\s*\{[^}]*overflow-x:\s*hidden;/s,
    "the message-list scroller never acquires a horizontal axis");
assert.match(styles, /\.user-message-list \.entry\s*\{[^}]*min-width:\s*0;/s,
    "message rows are allowed to shrink inside the sheet");

console.log("web user-message filter tests passed");
