// Clawdline's terminal layer: list iTerm2 sessions, send text into one of them.
//
// Why AppleScript rather than writing to the pty: you cannot write into another process's
// TTY on modern macOS (TIOCSTI is gone). iTerm2's `write text` is supported, stable, and
// does not require bringing the window forward — which is the entire point of this tool.
//
// Usage: osascript -l JavaScript iterm.js <list|current|send|key|capture|tails|reveal|newtab|close> [args...]

function run(argv) {
  const cmd = argv[0] || "list";

  let it;
  try {
    it = Application("iTerm2");
    // For a list, a stopped app is an observed absence rather than an enumeration failure. The
    // distinction matters to the watcher: this answer may remove old iTerm rows, while a broken
    // Apple-event bridge may not.
    if (!it.running()) return JSON.stringify(cmd === "list"
      ? { ok: true, complete: true, appRunning: false, sessions: [] }
      : { ok: false, error: "iTerm2 is not running" });
  } catch (e) {
    return JSON.stringify(cmd === "list"
      ? { ok: false, complete: false, sessions: [], error: "Cannot reach iTerm2: " + e.message }
      : { ok: false, error: "Cannot reach iTerm2: " + e.message });
  }

  let lastWalk = null;

  function eachSession(fn) {
    const walk = { failures: 0, examples: [] };
    lastWalk = walk;
    function failed(where, error) {
      walk.failures += 1;
      if (walk.examples.length < 3) {
        walk.examples.push(where + ": " + String(error && error.message || error || "unreadable"));
      }
    }
    let wins;
    try { wins = it.windows(); }
    catch (e) { failed("windows", e); return undefined; }
    for (let i = 0; i < wins.length; i++) {
      let tabs;
      try { tabs = wins[i].tabs(); }
      catch (e) { failed("window " + i + " tabs", e); continue; }
      for (let j = 0; j < tabs.length; j++) {
        let ss;
        try { ss = tabs[j].sessions(); }
        catch (e) { failed("window " + i + " tab " + j + " sessions", e); continue; }
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
      const id = safe(function () { return s.id(); }, "");
      const tty = safe(function () { return s.tty(); }, "");
      // A row without either identity cannot safely refresh an old row and must not count as a
      // successfully observed session. Keep the good rows beside it and lower the confidence of
      // the whole inventory instead.
      if (!id || !tty) {
        lastWalk.failures += 1;
        if (lastWalk.examples.length < 3) lastWalk.examples.push("session identity unreadable");
        return undefined;
      }
      out.push({
        id: id,
        name: safe(function () { return s.name(); }, ""),
        tty: tty,
        profile: safe(function () { return s.profileName(); }, ""),
        win: wi,
        tab: ti
      });
    });
    const complete = lastWalk.failures === 0;
    const result = { ok: complete, complete: complete, appRunning: true, sessions: out };
    if (!complete) {
      result.error = "iTerm2 session enumeration incomplete (" + lastWalk.failures +
        (lastWalk.failures === 1 ? " failure): " : " failures): ") + lastWalk.examples.join("; ");
    }
    return JSON.stringify(result);
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

    // The tail of what we pasted, whitespace removed so a wrapped line still matches. A
    // briefing ends in a 64-character secret, so this is unique on any screen; a message
    // shorter than the window is matched whole.
    const needle = text.replace(/\s+/g, "").slice(-24);

    // Is that tail still sitting in the composer? Only the composer is examined, not the
    // whole screen: an assistant that *did* accept the paste often echoes it back as a user
    // turn, and a match there would mean the opposite of a match below the prompt mark. When
    // the prompt mark cannot be found — a paste taller than the window pushed it off — this
    // answers no, because sending a Return nobody asked for is worse than sending none.
    function stillInComposer(s) {
      if (!needle) return false;
      const lines = safe(function () { return s.text(); }, "").replace(/\s+$/, "").split("\n");
      let mark = -1;
      for (let i = lines.length - 1; i >= 0 && i >= lines.length - 12; i--) {
        const head = lines[i].replace(/^[\s\u2502\u2503|]+/, "").charAt(0);
        if (head === ">" || head === "\u203a") { mark = i; break; }
      }
      if (mark < 0) return false;
      return lines.slice(mark).join("").replace(/\s+/g, "").indexOf(needle) >= 0;
    }

    const hit = eachSession(function (s) {
      if (safe(function () { return s.id(); }, "").toUpperCase() !== want) return undefined;
      // Bracketed paste, so multi-line text arrives as one paste instead of several Returns.
      // Without it, a two-line prompt submits itself after the first line.
      s.write({ text: ESC + "[200~" + text + ESC + "[201~", newline: false });
      if (submit) {
        pause(0.06);
        s.write({ text: CR, newline: false });
        // **Then look, rather than trust the timing.** Every reproducible case on this Mac
        // submits on that Return — measured across 40B, 1KB and 5KB payloads, against both
        // assistants, warm and cold-started, and at gaps from 60ms to 600ms. And yet three
        // dispatched Codex children in a row were found with an entire briefing sitting in
        // the composer, waiting for a person to press it. Whatever swallows that Return could
        // not be reproduced on demand, so this does not try to out-wait it: it checks, and
        // sends another only when the paste is demonstrably still there.
        //
        // Three looks, a beat apart. One nudge was enough every time it was needed, but a
        // failure nobody can summon is not one to trust a single reading about. Each round
        // checks before it writes, so a composer that has already submitted is never handed
        // a Return it did not ask for — which is the half worth being careful about, since
        // a stray Return lands on whatever the assistant is showing by then.
        for (let attempt = 0; attempt < 3; attempt++) {
          pause(attempt === 0 ? 0.25 : 0.4);
          if (!stillInComposer(s)) break;
          s.write({ text: CR, newline: false });
        }
      }
      return true;
    });
    return JSON.stringify(hit ? { ok: true } : { ok: false, error: "That session is gone" });
  }

  // Key bytes, written together and outside any bracketed paste. Inside one they would be text
  // handed to the program rather than a key it is being asked about, and the only reason to send
  // 0x16 is so that the far side treats it as Ctrl-V and reaches for the clipboard.
  if (cmd === "key") {
    const want = String(argv[1] || "").toUpperCase();
    const codes = argv.slice(2).map(function (raw) { return parseInt(String(raw || "0"), 10); });
    const hit = eachSession(function (s) {
      if (safe(function () { return s.id(); }, "").toUpperCase() !== want) return undefined;
      s.write({ text: String.fromCharCode.apply(String, codes), newline: false });
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

  // The tail of several sessions at once. The session list asks what every tab is doing, and
  // one `capture` per tab is one osascript process and one Apple event bridge per tab — four
  // of them behind every refresh of a four-tab list. This walks the windows once instead.
  //
  // Trailing blank lines go first, then the tail is taken: iTerm2 hands over the whole visible
  // grid, blank rows below the cursor included, so "the last 60 lines" of a half-filled screen
  // would otherwise be mostly nothing.
  if (cmd === "tails") {
    const want = String(argv[1] || "").toUpperCase().split(",").filter(function (s) { return s; });
    const lines = parseInt(String(argv[2] || "60"), 10) || 60;
    const out = {};
    eachSession(function (s) {
      const id = safe(function () { return s.id(); }, "").toUpperCase();
      if (want.indexOf(id) === -1) return undefined;
      const rows = safe(function () { return s.text(); }, "").split("\n");
      while (rows.length && !rows[rows.length - 1].trim()) rows.pop();
      out[id] = rows.slice(Math.max(0, rows.length - lines)).join("\n");
      return undefined;   // keep walking: every session that was asked for, not the first one
    });
    return JSON.stringify({ ok: true, tails: out });
  }

  // `reveal <id> [activate]`. Selecting the tab and bringing iTerm2 to the front are two
  // different things, and the second one is not always wanted: the prompt bar follows your
  // target as you move through its list, and a terminal that jumped in front on every press
  // would take the keyboard away from the box you were typing into.
  if (cmd === "reveal") {
    const want = String(argv[1] || "").toUpperCase();
    const activate = String(argv[2] === undefined ? "1" : argv[2]) === "1";
    const hit = eachSession(function (s, wi, ti, win, tab) {
      if (safe(function () { return s.id(); }, "").toUpperCase() !== want) return undefined;
      try { win.select(); } catch (e) {}
      try { tab.select(); } catch (e) {}
      try { s.select(); } catch (e) {}
      return true;
    });
    if (hit && activate) {
      try { it.activate(); } catch (e) {}
    }
    return JSON.stringify(hit ? { ok: true } : { ok: false, error: "That session is gone" });
  }

  // close <id>
  //
  // Close a session's tab. **The most destructive thing this file can do**, and the only one
  // whose effect the person asking for it cannot see: they are holding a phone, and what closes
  // is a window on a Mac that may have something else in it.
  //
  // So it closes the *session*, not the tab or the window. In iTerm2 a tab can be split, and
  // `tab.close()` would take the panes beside it — which belong to work nobody asked about. If
  // that session was the only one in its tab, iTerm2 removes the tab itself, which is the
  // behaviour somebody expects; if it was not, the split it was in survives.
  if (cmd === "close") {
    const want = String(argv[1] || "").toUpperCase();
    const hit = eachSession(function (s) {
      if (safe(function () { return s.id(); }, "").toUpperCase() !== want) return undefined;
      try { s.close(); } catch (e) { return "close failed: " + e.message; }
      return true;
    });
    if (hit === true) return JSON.stringify({ ok: true });
    return JSON.stringify({ ok: false, error: typeof hit === "string" ? hit : "That session is gone" });
  }

  // newtab <line>
  //
  // Open a tab and type one line into it. Used by the remote API so a session can be started
  // from a phone — the one operation that is not "talk to a session that already exists".
  //
  // The line is typed, not exec'd: iTerm2 gives you a shell and this writes into it, which is why
  // `cd` and the command arrive as one line rather than as a working directory setting. **It
  // arrives already quoted.** It used to be assembled here out of a directory and a command, and
  // that put the string that actually runs on the far side of an osascript call where nothing on
  // the Swift side could look at it; it is built by StartPoints.itermLine now.
  //
  // Nothing here calls activate(). Whoever asked for this is holding a phone, and the person at
  // the Mac — if there is one — is in the middle of something else.
  if (cmd === "newtab") {
    const line = String(argv[1] || "");
    if (!line) return JSON.stringify({ ok: false, error: "Nothing to run" });
    let w, t;
    try {
      w = it.currentWindow();
      t = w.createTabWithDefaultProfile();
    } catch (e) {
      // No window open at all — the case that happens when the Mac has been left alone and
      // iTerm2 is running with everything closed.
      try {
        w = it.createWindowWithDefaultProfile();
        t = w.currentTab();
      } catch (e2) {
        return JSON.stringify({ ok: false, error: "Could not open a tab: " + e2.message });
      }
    }
    let s;
    try { s = t.currentSession(); } catch (e) {
      return JSON.stringify({ ok: false, error: "The new tab has no session" });
    }

    try { s.write({ text: line, newline: true }); } catch (e) {
      return JSON.stringify({ ok: false, error: "Could not start it: " + e.message });
    }

    // The tty is what everything else keys on, and it is not there the instant the tab is.
    let tty = "";
    for (let i = 0; i < 20 && !tty; i++) {
      tty = safe(function () { return s.tty(); }, "");
      if (!tty) pause(0.05);
    }
    return JSON.stringify({
      ok: true,
      id: safe(function () { return s.id(); }, "").toUpperCase(),
      tty: tty
    });
  }

  return JSON.stringify({ ok: false, error: "Unknown command: " + cmd });
}

// Do not call this `delay` — JXA has a built-in global by that name, and shadowing it makes
// the whole script die at load time with an error that never mentions your code.
function pause(seconds) {
  ObjC.import("Foundation");
  $.NSThread.sleepForTimeInterval(seconds);
}
