// Pure data mapping. Native unseen-done belongs to agents(), never to raw children.
export const HISTORY_LIMIT = 64;
export const HISTORY_AGE = 24 * 60 * 60 * 1000;
export const normal = (status, child = false) => status === "error" ? "blocked"
  : child && status === "done" ? "idle" : status ?? null;

export const agentTitle = (agent, record) => agent.title || record?.report?.title ||
  agent.program?.name || agent.address;

export function scene(spaceList, paneList, agentList, raw) {
  const rawByAddress = new Map(raw.map(t => [t.address, t]));
  const list = agentList.map(a => ({ ...a, title: agentTitle(a, rawByAddress.get(a.address)), status: normal(a.status), child: false,
    machine: spaceList.find(s => s.id === a.space)?.machine ?? null,
    status_ms: rawByAddress.get(a.address)?.status_ms ?? null }));
  const mapped = new Map(list.map(a => [a.address, a]));
  // Repeat until stable so descendants work even if core order is child first.
  for (let pass = 0; pass < raw.length; pass++) {
    let added = false;
    for (const t of raw) {
      if (mapped.has(t.address) || !t.parent || t.state === "finished") continue;
      const parent = mapped.get(t.parent);
      if (!parent) continue;
      const status = normal(t.status ?? t.report?.state, true);
      const a = { address: t.address, pane: null, parent: t.parent, child: true,
        space: parent.space, space_name: parent.space_name, machine: parent.machine,
        tab: parent.tab, tab_name: parent.tab_name, tab_index: parent.tab_index,
        status, title: agentTitle({ title: t.title, program: t.program, address: t.address }, t),
        program: t.program, status_ms: t.status_ms ?? null, focused: false };
      mapped.set(a.address, a); list.push(a); added = true;
    }
    if (!added) break;
  }
  const blockers = list.filter(a => a.status === "blocked").sort((a, b) =>
    (b.status_ms ?? 0) - (a.status_ms ?? 0) || a.address.localeCompare(b.address));
  const descendants = new Map();
  for (const a of list.filter(a => a.child)) {
    const seen = new Set();
    let parent = a.parent;
    while (parent && !seen.has(parent)) {
      seen.add(parent);
      if (!descendants.has(parent)) descendants.set(parent, []);
      descendants.get(parent).push(a);
      parent = mapped.get(parent)?.parent;
    }
  }
  return { spaces: spaceList, panes: paneList, list, mapped, rawByAddress, blockers, descendants,
    blinking: new Set(blockers.slice(0, 3).map(a => a.address)) };
}

export function remember(previous, current, raw, now) {
  const byAddress = new Map(previous.filter(a => a && typeof a.address === "string" &&
    Number.isFinite(a.lastSeen) && now - a.lastSeen < HISTORY_AGE).map(a => [a.address, a]));
  for (const a of current) byAddress.set(a.address, {
    address: a.address, title: a.title, space: a.space, space_name: a.space_name,
    tab: a.tab, tab_name: a.tab_name, tab_index: a.tab_index, machine: a.machine,
    program: a.program, child: a.child, parent: a.parent, lastSeen: now
  });
  const live = new Set(raw.map(t => t.address));
  return [...byAddress.values()].sort((a, b) => b.lastSeen - a.lastSeen).slice(0, HISTORY_LIMIT)
    .map(a => live.has(a.address) ? { ...a, missing: false, dead: false, ended: null }
      : { ...a, missing: true });
}

export function confirm(history, listing, now) {
  const rows = Array.isArray(listing) ? listing : listing?.terminals;
  const indexed = new Map((Array.isArray(rows) ? rows : []).map(t => [t.address, t]));
  return history.map(a => {
    if (!a.missing) return a;
    const t = indexed.get(a.address);
    const dead = t?.state === "finished";
    return { ...a, dead, ended: dead ? t.finished_ms ?? a.ended ?? now : null };
  });
}

export function ghosts(history) {
  return history.filter(a => a.missing).map(a => ({ ...a, focused: false,
    status: a.dead ? "dead" : "unavailable" }));
}

export function towers(data, history) {
  const all = ghosts(history);
  const spaces = data.spaces.map(s => ({ ...s, tabs: [...s.tabs] }));
  for (const a of all) {
    let s = spaces.find(s => s.id === a.space);
    if (!s) { s = { id: a.space, name: a.space_name, machine: a.machine, active: false, tabs: [] }; spaces.push(s); }
    if (!s.tabs.some(t => t.id === a.tab)) s.tabs.push({ id: a.tab, name: a.tab_name, active: false, settled: true });
  }
  return spaces.sort((a, b) => Number(!!a.machine) - Number(!!b.machine)).map(s => {
    const floors = s.tabs.map(t => {
      const windows = data.panes.filter(p => p.tab === t.id).map(p => data.mapped.get(p.terminal) ?? {
        address: p.terminal, pane: p.pane, title: p.title, status: p.terminal ? "shell" : "mod", program: p.program });
      windows.push(...all.filter(a => a.space === s.id && a.tab === t.id));
      const attic = 12 * Math.max(0, ...windows.map(a => data.descendants.get(a.address)?.length ?? 0));
      return { ...t, windows, height: 24 + attic };
    });
    const maxPanes = Math.max(1, ...floors.map(f => f.windows.length));
    return { ...s, floors, width: Math.max(104, 24 * floors.length + 16, 14 * maxPanes + 18),
      height: Math.max(70, floors.reduce((n, f) => n + f.height, 0) + 22 + Math.min(60, data.panes.filter(p => s.tabs.some(t => t.id === p.tab)).length * 3)) };
  });
}

// Fit the complete towers, including scaled roof signs, below the fixed HUD.
// A crowded city keeps its base geometry and scrolls instead of shrinking text.
export function skylineGeometry(blocks, band) {
  const river = blocks.some(s => s.machine);
  const total = blocks.reduce((n, s) => n + s.width, 0) +
    24 * Math.max(0, blocks.length - 1) + (river ? 116 : 0);
  const tallest = Math.max(1, ...blocks.map(s => s.height + 54));
  const availableHeight = Math.max(1, band.height - 40 - 24);
  const availableWidth = Math.max(1, band.width - 48);
  const scale = Math.max(1, Math.min(2.5, availableHeight / tallest, availableWidth / Math.max(1, total)));
  return { scale, width: Math.max(band.width, total * scale + 48),
    height: Math.max(band.height - 40, tallest * scale + 24), river };
}
