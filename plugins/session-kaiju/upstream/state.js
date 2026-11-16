// Kaiju City 的纯逻辑：素材路径、昼夜判断、space→楼位映射、楼的损坏程度
import { spaces, shared, setShared } from "herdr";
import M from "./manifest.js";

const ASSETS = "mods/kaiju/assets";
const MIN = 60 * 1000;
// 连续 working 多久升一级损坏
export const LIGHT_AFTER = 2 * MIN;
export const HEAVY_AFTER = 8 * MIN;
// 回到 idle 后每级修复间隔
export const REPAIR_STEP = 650;

// 主题背景色偏暗 → 夜景素材
export function isNight(t) {
  const hex = (t.background ?? "#ffffff").slice(1);
  const [r, g, b] = [0, 2, 4].map((i) => parseInt(hex.slice(i, i + 2), 16));
  return 0.299 * r + 0.587 * g + 0.114 * b < 110;
}

// 像素硬阴影的颜色
export const shadowColor = (t) => (isNight(t) ? "#00000099" : "#2b262033");

// 状态 → 头像表情帧
export const FACE = { idle: "idle", working: "smash", done: "roar", blocked: "wave" };

// 在布局树里按 id 找节点
export function find(node, id) {
  if (node?.id === id) return node;
  for (const child of node?.children ?? []) {
    const found = find(child, id);
    if (found) return found;
  }
  return null;
}

export const asset = (t, name) => `${ASSETS}/${isNight(t) ? "night" : "day"}/${name}`;

// space 按顺序占前 8 个楼位；之后的不上沙盘
export function slotOf(spaceId) {
  const i = spaces().findIndex((s) => s.id === spaceId);
  return i >= 0 && i < M.slots.length ? i : null;
}

export const slotColor = (i) => (i != null && M.slots[i] ? M.slots[i].color : "#9b958c");

// space 里最早开始 working 的时间
const workingSince = (s) => {
  const ts = s.tabs.map((t) => t.working_since).filter((v) => v != null);
  return ts.length ? Math.min(...ts) : null;
};

// 按 space 里各 Agent 的状态算出「应有」的损坏程度（不用 space.status：blocked 会盖过 done）：
// 有 done = 3（废墟）；有 working 按时长 0/1/2；只有 blocked 保持现状；其余目标为 0（由 Damage 逐级修复）
function target(s, st, now, prev) {
  if (st.has("done")) return 3;
  if (st.has("working")) {
    const since = workingSince(s);
    const e = since == null ? 0 : now - since;
    return e >= HEAVY_AFTER ? 2 : e >= LIGHT_AFTER ? 1 : 0;
  }
  if (st.has("blocked")) return prev;
  return 0;
}

// 每个 space 当前的损坏程度；需要随时间变化时用一次性 timer.after 唤醒重绘（不用 every）
export class Damage {
  constructor() {
    this.level = new Map();
    this.wakeAt = Infinity;
  }

  // 返回 Map<spaceId, 0..3>，并按需预约下一次重绘
  // list：当前 Agent 列表（agents() 或演示数据）
  update(cx, list) {
    const now = Date.now();
    const bySpace = new Map();
    for (const a of list) {
      if (!bySpace.has(a.space)) bySpace.set(a.space, new Set());
      bySpace.get(a.space).add(a.status);
    }
    let next = Infinity;
    const live = new Set();
    for (const s of spaces()) {
      live.add(s.id);
      const prev = this.level.get(s.id) ?? 0;
      const st = bySpace.get(s.id) ?? new Set();
      const want = target(s, st, now, prev);
      let cur = want;
      if (want < prev && !st.has("working")) {
        // 修复：一级一级降，每级 REPAIR_STEP
        const at = this.repairAt?.get(s.id);
        if (at == null) {
          (this.repairAt ??= new Map()).set(s.id, now + REPAIR_STEP);
          cur = prev;
        } else if (now >= at) {
          cur = prev - 1;
          if (cur > want) this.repairAt.set(s.id, now + REPAIR_STEP);
          else this.repairAt.delete(s.id);
        } else {
          cur = prev;
        }
        if (cur > want) next = Math.min(next, this.repairAt.get(s.id));
      } else {
        this.repairAt?.delete(s.id);
      }
      if (st.has("working") && !st.has("done")) {
        const since = workingSince(s);
        if (since != null) {
          if (now - since < LIGHT_AFTER) next = Math.min(next, since + LIGHT_AFTER);
          else if (now - since < HEAVY_AFTER) next = Math.min(next, since + HEAVY_AFTER);
        }
      }
      this.level.set(s.id, cur);
    }
    for (const id of [...this.level.keys()]) if (!live.has(id)) this.level.delete(id);
    // 只在比已预约的更早时才再预约一次
    if (next < Infinity && next < this.wakeAt) {
      this.wakeAt = next;
      cx.timer.after(Math.max(16, next - now + 20), (cx) => {
        this.wakeAt = Infinity;
        cx.notify();
      });
    }
    return this.level;
  }
}

// ---------- 怪兽 ----------
export const STATUS_TEXT = { idle: "睡觉中", working: "拆楼中", done: "拆完了", blocked: "等你" };
const MODE = { working: "smash", done: "roar", blocked: "wave" };
const BUBBLE = { roar: "bang", wave: "question" };
const KEY = "kaiju.monsters";

// Agent 按出现顺序依次分配阵容里的怪兽，循环使用；存在 shared 里，各部件看到同一只
export function assign(list) {
  const old = shared(KEY) ?? { map: {}, n: 0 };
  const map = {};
  let n = old.n ?? 0;
  let changed = false;
  for (const a of list) {
    if (old.map?.[a.address]) map[a.address] = old.map[a.address];
    else {
      map[a.address] = M.monsters[n % M.monsters.length].id;
      n += 1;
      changed = true;
    }
  }
  if (Object.keys(old.map ?? {}).some((k) => !(k in map))) changed = true;
  if (changed) setShared(KEY, { map, n });
  return new Map(Object.entries(map));
}

export const monsterOf = (id) => M.monsters.find((m) => m.id === id) ?? M.monsters[0];

// 每只出场怪兽在沙盘里的去处：mode（smash/roar/wave）、脚底位置 spot、所属楼位 slot、头顶气泡
// idle 或所属 space 不在沙盘上的：slot = null，不出场
export function behaviors(list) {
  const out = new Map();
  const perSlot = new Map();
  for (const a of list) {
    const slot = slotOf(a.space);
    const mode = MODE[a.status] ?? null;
    if (mode == null || slot == null) {
      out.set(a.address, { mode: "idle", spot: null, slot: null, bubble: null });
      continue;
    }
    const k = perSlot.get(slot) ?? 0;
    perSlot.set(slot, k + 1);
    // 每栋楼有 2~4 个站位；更多的怪兽轮流用这些站位，每轮往画面前方（下方）挪一点
    const st = M.slots[slot].stand;
    const p = st[k % st.length];
    const round = Math.floor(k / st.length);
    const spot = { x: p.x, y: p.y + round * 6, depth: p.depth + round * 12 };
    out.set(a.address, { mode, spot, slot, bubble: BUBBLE[mode] ?? null });
  }
  return out;
}
