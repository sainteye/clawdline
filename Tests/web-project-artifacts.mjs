import assert from "node:assert/strict";
import { isOpenableProjectLink, isServedProjectArtifact } from
    "../Resources/web/app/js/input/project-links.js";

assert.equal(isOpenableProjectLink("https://clawdline.com"), true,
    "ordinary web links remain openable");
assert.equal(isOpenableProjectLink("/v1/sessions/SESSION-1/artifacts/backlog"), true,
    "the authenticated backlog route opens on the same origin");
assert.equal(isOpenableProjectLink("/v1/sessions/SESSION-1/artifacts/milestone"), true,
    "the authenticated milestone route opens on the same origin");
assert.equal(isServedProjectArtifact("/v1/sessions/SESSION-1/artifacts/milestone"), true,
    "the client can label a broker-served artifact without learning its file path");
assert.equal(isOpenableProjectLink("file:///Users/you/private.html"), false,
    "a filesystem path never becomes a phone link");
assert.equal(isOpenableProjectLink("javascript:alert(1)"), false,
    "script URLs stay inert");
assert.equal(isOpenableProjectLink("/v1/sessions/SESSION-1/artifacts/../secret"), false,
    "only the two typed artifact slots are openable");

console.log("web project artifacts: 7 assertions passed");
