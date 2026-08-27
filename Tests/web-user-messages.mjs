import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

// Load the shipped `.js` as an ES module without depending on a repository-wide package type.
// Browsers already know it is a module from its import edge; this data URL gives Node the same
// fact and keeps the test runnable in a clean archive by itself.
const source = await readFile(
    new URL("../Resources/web/app/js/view/user-messages-data.js", import.meta.url),
    "utf8"
);
const data = await import("data:text/javascript;base64," + Buffer.from(source).toString("base64"));
const { copyForUserMessages, userMessageEntries, userMessagePosition } = data;

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
assert.equal(copyForUserMessages("zh-CN").title, "我发出的消息");
assert.equal(copyForUserMessages("en-US").title, "My messages");
assert.equal(copyForUserMessages("ja-JP").title, "My messages", "unknown locales use English");

console.log("web user-message filter tests passed");
