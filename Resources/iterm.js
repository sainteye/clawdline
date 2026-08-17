// Clawdline's terminal layer: list iTerm2 sessions, send text into one of them.
//
// Why AppleScript rather than writing to the pty: you cannot write into another process's
// TTY on modern macOS (TIOCSTI is gone). iTerm2's `write text` is supported, stable, and
// does not require bringing the window forward — which is the entire point of this tool.
//
// Usage: osascript -l JavaScript iterm.js <list|current|send|capture|reveal> [args...]

function run(argv) {
  const cmd = argv[0] || "list";

  let it;
  try {
    it = Application("iTerm2");
    if (!it.running()) return JSON.stringify({ ok: false, error: "iTerm2 is not running" });
  } catch (e) {
    return JSON.stringify({ ok: false, error: "Cannot reach iTerm2: " + e.message });
  }

  function eachSession(fn) {
    const wins = it.windows();
    for (let i = 0; i < wins.length; i++) {
      let tabs;
      try { tabs = wins[i].tabs(); } catch (e) { continue; }
      for (let j = 0; j < tabs.length; j++) {
        let ss;
        try { ss = tabs[j].sessions(); } catch (e) { continue; }
        for (let k = 0; k < ss.length; k++) {
          const r = fn(ss[k], i, j, wins[i], tabs[j]);
          if (r !== undefined) return r;
        }
      }
    }
    return undefined;
  }

  function safe(fn, dflt) {
    try { const v = fn(); return v === null || v === undefined ? dflt : String(v); }
    catch (e) { return dflt; }
  }

  if (cmd === "list") {
    const out = [];
    eachSession(function (s, wi, ti) {
      out.push({
        id: safe(function () { return s.id(); }, ""),
        name: safe(function () { return s.name(); }, ""),
        tty: safe(function () { return s.tty(); }, ""),
        profile: safe(function () { return s.profileName(); }, ""),
        win: wi,
        tab: ti
      });
    });
    return JSON.stringify({ ok: true, sessions: out });
  }

  if (cmd === "current") {
    try {
      const s = it.currentWindow().currentSession();
      return JSON.stringify({ ok: true, id: String(s.id()) });
    } catch (e) {
      return JSON.stringify({ ok: false, error: "No active session" });
    }
  }

  if (cmd === "send") {
    const want = String(argv[1] || "").toUpperCase();
    const text = argv[2] === undefined ? "" : String(argv[2]);
    const submit = String(argv[3] || "1") === "1";
    const ESC = String.fromCharCode(27);
    const CR = String.fromCharCode(13);

    const hit = eachSession(function (s) {
      if (safe(function () { return s.id(); }, "").toUpperCase() !== want) return undefined;
      // Bracketed paste, so multi-line text arrives as one paste instead of several Returns.
      // Without it, a two-line prompt submits itself after the first line.
      s.write({ text: ESC + "[200~" + text + ESC + "[201~", newline: false });
      if (submit) {
        pause(0.06);
        s.write({ text: CR, newline: false });
      }
      return true;
    });
    return JSON.stringify(hit ? { ok: true } : { ok: false, error: "That session is gone" });
  }

  // One byte, written outside any bracketed paste. Inside one it would be a character being
  // handed to the program rather than a key it is being asked about, and the only reason to send
  // 0x16 is so that the far side treats it as Ctrl-V and reaches for the clipboard.
  if (cmd === "key") {
    const want = String(argv[1] || "").toUpperCase();
    const code = parseInt(String(argv[2] || "0"), 10);
    const hit = eachSession(function (s) {
      if (safe(function () { return s.id(); }, "").toUpperCase() !== want) return undefined;
      s.write({ text: String.fromCharCode(code), newline: false });
      return true;
    });
    return JSON.stringify(hit ? { ok: true } : { ok: false, error: "That session is gone" });
  }

  if (cmd === "capture") {
    // `text` is the *visible* screen only — iTerm2's AppleScript has no scrollback.
    // tmux can go further back, which is why that path passes -S.
    const want = String(argv[1] || "").toUpperCase();
    const hit = eachSession(function (s) {
      if (safe(function () { return s.id(); }, "").toUpperCase() !== want) return undefined;
      return safe(function () { return s.text(); }, "");
    });
    return JSON.stringify(hit === undefined
      ? { ok: false, error: "That session is gone" }
      : { ok: true, text: hit });
  }

  if (cmd === "reveal") {
    const want = String(argv[1] || "").toUpperCase();
    const hit = eachSession(function (s, wi, ti, win, tab) {
      if (safe(function () { return s.id(); }, "").toUpperCase() !== want) return undefined;
      try { win.select(); } catch (e) {}
      try { tab.select(); } catch (e) {}
      try { s.select(); } catch (e) {}
      return true;
    });
    if (hit) {
      try { it.activate(); } catch (e) {}
    }
    return JSON.stringify(hit ? { ok: true } : { ok: false, error: "That session is gone" });
  }

  return JSON.stringify({ ok: false, error: "Unknown command: " + cmd });
}

// Do not call this `delay` — JXA has a built-in global by that name, and shadowing it makes
// the whole script die at load time with an error that never mentions your code.
function pause(seconds) {
  ObjC.import("Foundation");
  $.NSThread.sleepForTimeInterval(seconds);
}
