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
const { copyForUserMessages, userMessageEntries } = data;

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
    "first question",
    "second question",
    "still sending"
]);
assert.equal(messages[0], transcript[1], "entries are not copied or rewritten");
assert.deepEqual(userMessageEntries(null, undefined), []);

assert.equal(copyForUserMessages("zh-TW").title, "我傳出的訊息");
assert.equal(copyForUserMessages("zh-Hant").empty, "你還沒有在這個 session 傳出訊息。");
assert.equal(copyForUserMessages("zh-CN").title, "我发出的消息");
assert.equal(copyForUserMessages("en-US").title, "My messages");
assert.equal(copyForUserMessages("ja-JP").title, "My messages", "unknown locales use English");

console.log("web user-message filter tests passed");
