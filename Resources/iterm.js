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

  // One property of every session, in one Apple event.
  //
  // **This is the whole performance story of this file, and it is a story about the tail.**
  // `eachSession` above asks each session for each property separately, and a JXA property
  // access is one synchronous round trip to iTerm2's main thread — 17 sessions × 4 properties
  // is 60-odd of them. A specifier chain (`it.windows.tabs.sessions.tty`) is one event for the
  // whole nested array, so the same inventory costs 4.
  //
  // **On an idle Mac this barely shows**: 0.16 s against 0.11 s, and anyone measuring it on a
  // quiet machine will conclude it was not worth doing. The reading that matters was taken with
  // four assistants streaming into seventeen tabs — the state this app is used in — where the
  // walk measured 1.59, 2.52, 3.56, 3.88 and 5.36 s against the chain's 0.47 to 1.04. Each of
  // those 60 round trips can be delayed on its own, so the walk degrades with the *product* of
  // load and session count while the chain degrades with load alone. What batching buys is not a
  // faster median; it is a p99 that does not run away.
  //
  // And the wall clock here is not the point either. iTerm2 answers Apple events on the thread
  // it also draws with, and the watcher re-reads this inventory every 1.2 s, so what is saved is
  // mostly given back to the terminals you were trying to read in the first place.
  //
  // Returns a `[window][tab][session]` array, or `null` when the chain failed — every caller
  // falls back to `eachSession`, which keeps its own failure accounting.
  function bulk(prop) {
    try {
      const v = it.windows.tabs.sessions[prop]();
      return Array.isArray(v) ? v : null;
    } catch (e) { return null; }
  }

  // The same shape for several properties at once, with the tree checked for agreement. A
  // property that came back a different shape than `id` cannot be zipped against it, and
  // guessing which row it belongs to would put one session's tty on another's row — so the
  // whole batch is refused and the caller walks instead.
  function bulkTree(props) {
    const first = bulk(props[0]);
    if (!first) return null;
    const out = {};
    out[props[0]] = first;
    for (let p = 1; p < props.length; p++) {
      const v = bulk(props[p]);
      if (!v || v.length !== first.length) return null;
      for (let i = 0; i < first.length; i++) {
        if (!Array.isArray(v[i]) || !Array.isArray(first[i])
            || v[i].length !== first[i].length) return null;
        for (let j = 0; j < first[i].length; j++) {
          if (!Array.isArray(v[i][j]) || !Array.isArray(first[i][j])
              || v[i][j].length !== first[i][j].length) return null;
        }
      }
      out[props[p]] = v;
    }
    return out;
  }

  function cell(tree, prop, i, j, k) {
    const v = tree[prop][i][j][k];
    return v === null || v === undefined ? "" : String(v);
  }

  // Where a session lives, found from one batched `id` read instead of a walk that asks every
  // session in front of it for its own. Returns the session specifier, or `undefined`.
  //
  // **The identity is re-read through the specifier before it is handed back**, so a tab that
  // closed between the batch and the write cannot silently redirect a keystroke into whatever
  // slid into its index. That is the same guarantee the walk gives — it compares an id too —
  // for one round trip instead of one per session ahead of the target.
  function locate(wantId) {
    const ids = bulk("id");
    if (!ids) return undefined;
    for (let i = 0; i < ids.length; i++) {
      if (!Array.isArray(ids[i])) continue;
      for (let j = 0; j < ids[i].length; j++) {
        if (!Array.isArray(ids[i][j])) continue;
        for (let k = 0; k < ids[i][j].length; k++) {
          const id = ids[i][j][k];
          if (!id || String(id).toUpperCase() !== wantId) continue;
          let s, win, tab;
          try {
            win = it.windows[i];
            tab = win.tabs[j];
            s = tab.sessions[k];
          } catch (e) { return undefined; }
          if (safe(function () { return s.id(); }, "").toUpperCase() !== wantId) return undefined;
          return { session: s, win: win, tab: tab };
        }
      }
    }
    return undefined;
  }

  // Find one session and do something with it. Batched first; the walk is the fallback, and it
  // is still the only path when the specifier chain is unavailable.
  function withSession(wantId, fn) {
    const found = locate(wantId);
    if (found) return fn(found.session, found.win, found.tab);
    return eachSession(function (s, wi, ti, win, tab) {
      if (safe(function () { return s.id(); }, "").toUpperCase() !== wantId) return undefined;
      return fn(s, win, tab);
    });
  }

  if (cmd === "list") {
    const out = [];
    const props = ["id", "tty", "name", "profileName"];
    const tree = bulkTree(props);
    if (tree) {
      let failures = 0;
      for (let i = 0; i < tree.id.length; i++) {
        for (let j = 0; j < tree.id[i].length; j++) {
          for (let k = 0; k < tree.id[i][j].length; k++) {
            const id = cell(tree, "id", i, j, k);
            const tty = cell(tree, "tty", i, j, k);
            // Same rule as the walk below, and it turns on the *id*: a row nothing can name
            // cannot safely refresh an old row, so it is dropped and the whole inventory loses
            // confidence instead.
            //
            // **An empty tty is a different fact and no longer counts as one of those.** Under
            // iTerm2's own tmux control mode (`tmux -CC`) each tmux window is drawn as an
            // ordinary iTerm2 tab whose pty belongs to tmux — measured here as one identified
            // row with `tty: ""` per tmux window, while `tmux list-panes` held the real
            // `/dev/ttys009` that iTerm2 never mentions. Dropping those rows made a single
            // control-mode session enough to mark the whole inventory incomplete, and an
            // incomplete inventory refuses every close on this Mac. So the row travels with a
            // marker instead, and Swift decides — against tmux's own client list, which is a
            // second source — whether tmux really drew it.
            if (!id) { failures += 1; continue; }
            const row = {
              id: id,
              name: cell(tree, "name", i, j, k),
              tty: tty,
              profile: cell(tree, "profileName", i, j, k),
              win: i,
              tab: j
            };
            if (!tty) { row.ptyless = true; }
            out.push(row);
          }
        }
      }
      const complete = failures === 0;
      const result = { ok: complete, complete: complete, appRunning: true, sessions: out };
      if (!complete) {
        result.error = "iTerm2 session enumeration incomplete (" + failures +
          (failures === 1 ? " failure): " : " failures): ") + "session identity unreadable";
      }
      return JSON.stringify(result);
    }
    eachSession(function (s, wi, ti) {
      const id = safe(function () { return s.id(); }, "");
      // **A question that threw and a question answered "none" are two different facts.**
      // `safe()` spells both `""`, which is right everywhere else in this file and wrong here:
      // it put an iTerm2 session whose tty could not be read into exactly the shape of a tmux
      // control-mode window, and with any control-mode client on the Mac, Swift would then
      // attribute that row to tmux and say nothing about it. So the throw is caught on its own
      // and counted like an unreadable id, while `null`, `undefined` and `""` all stay the empty
      // answer the marker is for — the same reading `cell()` gives the batched branch above,
      // which cannot tell those three apart and must not be made to disagree with this one.
      let tty = "";
      let ttyUnreadable = false;
      try {
        const value = s.tty();
        tty = value === null || value === undefined ? "" : String(value);
      } catch (e) { ttyUnreadable = true; }
      // The same rule as the batched branch above, for the same reasons: no id is unreadable and
      // lowers the confidence of the whole inventory, while no tty is a tmux control-mode window
      // and travels with a marker for Swift to check against tmux.
      if (!id) {
        lastWalk.failures += 1;
        if (lastWalk.examples.length < 3) lastWalk.examples.push("session identity unreadable");
        return undefined;
      }
      if (ttyUnreadable) {
        lastWalk.failures += 1;
        if (lastWalk.examples.length < 3) lastWalk.examples.push("session pty unreadable");
        return undefined;
      }
      const row = {
        id: id,
        name: safe(function () { return s.name(); }, ""),
        tty: tty,
        profile: safe(function () { return s.profileName(); }, ""),
        win: wi,
        tab: ti
      };
      if (!tty) { row.ptyless = true; }
      out.push(row);
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

    const hit = withSession(want, function (s) {
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
    const hit = withSession(want, function (s) {
      s.write({ text: String.fromCharCode.apply(String, codes), newline: false });
      return true;
    });
    return JSON.stringify(hit ? { ok: true } : { ok: false, error: "That session is gone" });
  }

  if (cmd === "capture") {
    // `text` is the *visible* screen only — iTerm2's AppleScript has no scrollback.
    // tmux can go further back, which is why that path passes -S.
    const want = String(argv[1] || "").toUpperCase();
    const hit = withSession(want, function (s) {
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
    function keep(id, text) {
      if (want.indexOf(id) === -1) return;
      const rows = text.split("\n");
      while (rows.length && !rows[rows.length - 1].trim()) rows.pop();
      out[id] = rows.slice(Math.max(0, rows.length - lines)).join("\n");
    }
    // Batch the *identities*, then read only the screens that were asked for.
    //
    // **Not `bulkTree(["id", "text"])`, and the measurement is why.** Batching text as well is
    // one Apple event instead of one per tab, and it was 1.61 s against the walk's 0.16 s: a
    // screen is kilobytes, seventeen of them are the payload rather than the handshake, and
    // sixteen were going to be thrown away. `list` batches four *scalar* properties and wins ten
    // times over; the rule those two readings agree on is that the handshake is what batching
    // buys back, so batch what every row needs and address what only some rows need.
    const ids = bulk("id");
    if (ids) {
      for (let i = 0; i < ids.length; i++) {
        if (!Array.isArray(ids[i])) continue;
        for (let j = 0; j < ids[i].length; j++) {
          if (!Array.isArray(ids[i][j])) continue;
          for (let k = 0; k < ids[i][j].length; k++) {
            const id = String(ids[i][j][k] || "").toUpperCase();
            if (!id || want.indexOf(id) === -1) continue;
            const text = safe(function () { return it.windows[i].tabs[j].sessions[k].text(); }, "");
            keep(id, text);
          }
        }
      }
      return JSON.stringify({ ok: true, tails: out });
    }
    eachSession(function (s) {
      const id = safe(function () { return s.id(); }, "").toUpperCase();
      if (want.indexOf(id) === -1) return undefined;
      keep(id, safe(function () { return s.text(); }, ""));
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
    const hit = withSession(want, function (s, win, tab) {
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

  // `activate` — bring iTerm2 forward, without selecting anything inside it.
  //
  // The half of `reveal` that has no session id to work from. Under `tmux -CC` the selection is
  // tmux's to make: asking tmux for a window does move iTerm2's tab, because iTerm2 is following
  // the control-mode stream — what it does not do is bring iTerm2's *window* forward when
  // another application is in front. There is no supported way to name that tab from outside
  // (iTerm2's scripting dictionary exposes no tmux property at all), so this asks for the only
  // thing that can be asked for reliably: the application.
  if (cmd === "activate") {
    try { it.activate(); } catch (e) {
      return JSON.stringify({ ok: false, error: "Could not bring iTerm2 forward: " + e.message });
    }
    return JSON.stringify({ ok: true });
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
    const hit = withSession(want, function (s) {
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
