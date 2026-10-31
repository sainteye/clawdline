import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node source tests use the source extension.
import { nativeTerminalCommand } from "./native-terminal-command.ts"

test("all slash commands, including future names, can continue in the same CLI",()=>{
 for(const assistant of ["claude","codex"]) {
  assert.equal(nativeTerminalCommand("/model",assistant,"tmux"),true)
  assert.equal(nativeTerminalCommand("/future-command value",assistant,"iterm"),true)
  assert.equal(nativeTerminalCommand("explain /model",assistant,"tmux"),false)
  assert.equal(nativeTerminalCommand("$skill",assistant,"tmux"),false)
  assert.equal(nativeTerminalCommand("/model",assistant,"claude_remote"),false)
  assert.equal(nativeTerminalCommand("/model",assistant,"tmux",1),false)
 }
 assert.equal(nativeTerminalCommand("/model","antigravity","tmux"),false)
})

for (const assistant of ["claude", "codex"]) {
 assert.equal(nativeTerminalCommand("/resume",assistant,"tmux"),false)
 assert.equal(nativeTerminalCommand("/resume previous",assistant,"iterm"),false)
 assert.equal(nativeTerminalCommand("/resume-plugin",assistant,"tmux"),true)
}
