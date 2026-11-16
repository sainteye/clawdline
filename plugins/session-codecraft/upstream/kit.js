// World of Codecraft (village of Valbrume): shared bits of the mod. Palette, fonts, the hero
// every agent becomes (race from its address, class from its program), the
// hour of the day, the Claude quota fountains, and small wood/iron/parchment parts.

import { div, image, svg, PathBuilder, Background } from "gpui-kit";
import { h_flex } from "gpui-base";
import { home, readTextFile, panes } from "herdr";
import { t, tbl } from "./i18n.js";

export const ART = "mods/world-of-codecraft/art";
export const TITLE = "Cinzel"; // Roman capitals, headings only (fonts/, OFL)
export const UI = "Alegreya Sans"; // sturdy humanist text, everything else (fonts/, OFL)
export const VILLAGE = "Valbrume";

export const C = {
  wood: "#7a4e2a", woodLight: "#a8693a", woodPale: "#c98b52", woodDark: "#4a2e17", woodInk: "#2a1a0e",
  iron: "#3b3542", ironLight: "#6b6574", rivet: "#9a93a3",
  parch: "#efe2bf", parchDark: "#d9c697", ink: "#2e2016", inkSoft: "#6a5034",
  gold: "#e8c35a", goldLight: "#ffe9a0", quest: "#ffd23f", wax: "#a3242e",
  slate: "#16171c", stone: "#8d8a93", cobble: "#5d5866", cobbleLight: "#77717f", cobbleDark: "#45414d",
  water: "#4aa8e8", waterLight: "#a8dcff", mana: "#8a7bff",
  cream: "#fbf6e8", night: "#0b1030",
};

const RECT = () => PathBuilder.fill().move_to("0%", "0%").line_to("100%", "0%").line_to("100%", "100%").line_to("0%", "100%").close().build();
/** A layer filling its relative parent with a linear gradient (180 = top to bottom). */
export function gradient(from, to, angle = 180) {
  return globalThis.window.paint_path(RECT(), Background.linear_gradient(angle, from, to)).absolute().top(0).left(0).size_full();
}

export function hash(s) {
  let h = 2166136261;
  for (const ch of String(s)) h = Math.imul(h ^ ch.charCodeAt(0), 16777619);
  h ^= h >>> 16; h = Math.imul(h, 0x7feb352d); h ^= h >>> 15; h = Math.imul(h, 0x846ca68b); h ^= h >>> 16;
  return h >>> 0;
}

export function clip(s, n) {
  s = String(s ?? "");
  return s.length > n ? s.slice(0, Math.max(1, n - 1)) + "…" : s;
}

export function folder(cwd) {
  if (!cwd) return "";
  const h = home();
  return cwd === h ? "~" : cwd.startsWith(h + "/") ? "~" + cwd.slice(h.length) : cwd;
}

// ------------------------------------------------------------------ heroes

export const RACES = ["elfe", "orque", "humain", "mort-vivant", "nain"];
// Display names in the current language (the keys stay as the art file names use them).
export const RACE_FR = tbl("race");
const NAMES = {
  elfe: ["Aelith", "Sylvarin", "Elowen", "Thalindra", "Faelar", "Liriel", "Ithrandë"],
  orque: ["Grokka", "Murzag", "Ushna", "Thrakk", "Bolgra", "Zugor", "Krasha"],
  humain: ["Aldric", "Maëlle", "Gauvain", "Isolde", "Bertrand", "Ysaline", "Tancrède"],
  "mort-vivant": ["Morvane", "Ossian", "Cendrine", "Lugubre", "Sépulcrin", "Vespérine", "Fanel"],
  nain: ["Durgan", "Helga", "Ostrik", "Brunna", "Gromdal", "Barbefer", "Kordi"],
};

export const CLASSES = {
  mage: { get nom() { return t("cls").mage; }, bat: "tour", item: "baton" },
  artificier: { get nom() { return t("cls").artificier; }, bat: "atelier", item: "cle" },
  guerrier: { get nom() { return t("cls").guerrier; }, bat: "caserne", item: "marteau" },
  pretre: { get nom() { return t("cls").pretre; }, bat: "temple", item: "relique" },
  rodeur: { get nom() { return t("cls").rodeur; }, bat: "grange", item: "arc" },
  voleur: { get nom() { return t("cls").voleur; }, bat: "etal1", item: "dague" },
};

/** The class a program plays: Claude a mage, Codex an artificer, shells warriors… */
export function classOf(program) {
  const k = String(program?.key ?? "").toLowerCase();
  if (k.startsWith("claude")) return "mage";
  if (k === "codex") return "artificier";
  if (program?.shell || k === "shell") return "guerrier";
  if (k.includes("gemini")) return "pretre";
  if (k.includes("opencode") || k === "pi" || k.includes("amp") || k.includes("cursor")) return "rodeur";
  return "voleur";
}

/** Everything that makes a terminal a villager-hero, stable for its address. */
export function heroOf(address, program) {
  const h = hash(address);
  const race = RACES[h % RACES.length];
  const cls = classOf(program);
  const list = NAMES[race];
  const name = list[(h >>> 5) % list.length];
  return { race, cls, name, prog: program?.name ?? "agent", sprite: `${ART}/h-${race}-${cls}`, item: CLASSES[cls].item };
}

/** "Orc mage" / "mage orque". */
export const heroDesc = (h) => t("hero", RACE_FR[h.race], CLASSES[h.cls].nom);
export const heroTitle = (h) => `${h.name}, ${heroDesc(h)}`;

/** Per state: `mot` (the short word) and `fr` (the chronicle verb), in the current language. */
export const QUEST = tbl("quest");

/** Shell terminals (not agents) of every tab: the warriors on guard at the forge. */
export function guards() {
  return panes().filter((p) => p.terminal && !p.status && p.program?.shell);
}

// ------------------------------------------------------------------ time

const HOURS = [0, 5, 8, 12, 14, 18, 21]; // Matins, Lauds, Terce, Sext, None, Vespers, Compline
export function hourName(d) {
  const h = d.getHours();
  let n = 0;
  HOURS.forEach((at, i) => { if (h >= at) n = i; });
  return t("hours")[n];
}

/** The sky's phase and colors for a time. */
export function sky(d) {
  const h = d.getHours() + d.getMinutes() / 60;
  if (h >= 5 && h < 8) return { phase: "aube", top: "#3a3a72", bottom: "#f2a36b", dark: 0.22 };
  if (h >= 8 && h < 18) return { phase: "jour", top: "#5aa6e0", bottom: "#cfeaf6", dark: 0 };
  if (h >= 18 && h < 21) return { phase: "crepuscule", top: "#2e2462", bottom: "#ee8456", dark: 0.25 };
  return { phase: "nuit", top: "#070a20", bottom: "#1d2452", dark: 0.48 };
}
export const isNight = (d) => sky(d).phase !== "jour";

export const hhmm = (d) => `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;

// ------------------------------------------------------------------ quotas
// Claude accounts A and B, read only from the files the local poller writes.

const STALE = 30 * 60e3;
async function readQuota(acc) {
  try {
    const q = JSON.parse((await readTextFile(`${home()}/.local/state/claude-duo/quota-${acc}.json`)) ?? "null");
    if (!q?.rate_limits) return { acc, missing: true };
    const pick = (k) => {
      const w = q.rate_limits[k];
      return typeof w?.used_percentage === "number" ? { used: Math.round(w.used_percentage), resets: (w.resets_at ?? 0) * 1e3 } : null;
    };
    return { acc, at: q.at * 1e3, stale: Date.now() - q.at * 1e3 > STALE, week: pick("seven_day"), five: pick("five_hour") };
  } catch {
    return { acc, missing: true };
  }
}
export const readQuotas = () => Promise.all([readQuota("A"), readQuota("B")]);

/** "4d 12h" (FR "4j 12h"), "3h 05", "12 min". */
export function left(ms) {
  const d = ms - Date.now();
  if (!ms || d <= 0) return "";
  const m = Math.floor(d / 60e3), h = Math.floor(m / 60);
  if (h >= 48) return `${Math.floor(h / 24)}${t("days")} ${h % 24}h`;
  if (h >= 1) return `${h}h ${String(m % 60).padStart(2, "0")}`;
  return `${m} min`;
}

// ------------------------------------------------------------------ parts

/** An iron rivet. */
export const rivet = (s = 4) => div().size(s).flex_none().bg(C.rivet).border_1().border_color(C.iron).rounded(1);

/** A tinted pixel icon from art/ic-*.svg. */
export const icon = (name, color, w = 13) => {
  const el = svg(`${ART}/ic-${name}.svg`).w(w).h(Math.round((w * 7) / 9)).flex_none();
  return color ? el.text_color(color) : el;
};

/** A heraldic pennant: tinted cloth with a charge on it. */
export function pennant(color, charge, s = 2) {
  return div().relative().w(10 * s).h(14 * s).flex_none()
    .child(svg(`${ART}/fanion.svg`).absolute().left(0).top(0).w(10 * s).h(14 * s).text_color(color))
    .child(svg(`${ART}/blason-${charge}.svg`).absolute().left(Math.round(1.5 * s)).top(2 * s).w(7 * s).h(7 * s).text_color(C.goldLight));
}

const COLORS = ["#7a1f2b", "#2f4a7a", "#2f6b4a", "#6b3a8a", "#8a5a1f", "#1f6b6b", "#8a2f5a", "#4a5a2f"];
const CHARGES = ["etoile", "lune", "tour", "cle", "chene", "coupe", "epee", "soleil"];
const ROMAN = ["I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X", "XI", "XII"];

/** A space as a district: its colors, charge and name. */
export function district(sp) {
  const h = hash(sp.id);
  const raw = String(sp.name ?? "");
  const nom = raw === "~" ? t("home") : raw.replace(/^~\//, "").split("/").pop();
  return {
    color: COLORS[h % COLORS.length],
    charge: CHARGES[(h >>> 4) % CHARGES.length],
    num: ROMAN[(sp.index ?? 1) - 1] ?? String(sp.index),
    nom,
    titre: t("district", ROMAN[(sp.index ?? 1) - 1] ?? sp.index, nom),
  };
}

/** A small status sign drawn natively: "!" flashing, "?" glowing, dots for work. */
export function questMark(st, size = 14) {
  if (st === "blocked") return image(`${ART}/mark-quete.svg`).w(size * 0.5).h(size).flex_none().loop("opacity", { from: 0.25, to: 1, period: 520 });
  if (st === "done") return image(`${ART}/mark-rendu.svg`).w(size * 0.64).h(size).flex_none().loop("opacity", { from: 0.6, to: 1, period: 1400 });
  if (st === "working") return div().w(18).flex_none().font_family(UI).text_size(size).text_color("#8fd16a").frames(["·  ", "·· ", "···", " ··", "  ·", "   "], 240);
  return null;
}

/** A hero's little portrait (stand sprite) for lists. */
export const portrait = (hero, h = 24, sit = false) =>
  image(`${hero.sprite}${sit ? "-assis" : ""}.svg`).w(Math.round((h * 18) / 27)).h(h).flex_none();

export const stop = (_e, cx) => cx.stop_propagation();
