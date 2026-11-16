// Where things are in the baked world (art/monde, 3000x400 logical px; 1 art px = 1 logical px).
// Hand-edited: unit spots per building (feet points), the campfire ring, the road heroes walk on,
// building hit boxes (tooltip, double-click to recruit), chimneys and live glows.

import { t } from "./i18n.js";

/** Feet points where the heroes of each class stand while working / blocked / done (first free wins). */
export const SPOTS = {
  tour: [[690, 372], [640, 342], [745, 352], [470, 338], [600, 390]], // mage tower (mage)
  atelier: [[1010, 300], [935, 290], [1060, 272], [820, 296], [1090, 312]], // forge-workshop (artificer)
  caserne: [[2600, 338], [2470, 345], [2700, 318], [2380, 352], [2530, 368], [2660, 366], [2750, 345], [2430, 380]], // barracks (warrior, shells on guard)
  temple: [[2190, 372], [2235, 338], [2110, 388], [2290, 362], [2160, 330]], // temple (priest)
  grange: [[420, 345], [290, 300], [520, 312], [350, 378], [470, 380]], // barn and archery spot (ranger)
  etal1: [[2020, 395], [1735, 320], [1040, 318], [960, 316], [1190, 392]], // market stalls (rogue)
};

/** Scout spots at the forest edge near each class building (the "éclaireur" scene: reading, searching). */
export const LISIERE = {
  tour: [[455, 212], [480, 250], [430, 240]],
  atelier: [[1060, 172], [1095, 208], [1035, 210]],
  caserne: [[2640, 252], [2610, 215], [2690, 238]],
  temple: [[1722, 168], [1692, 202], [1750, 205]],
  grange: [[322, 182], [350, 226], [300, 222]],
  etal1: [[2250, 205], [2282, 242], [2225, 240]],
};

/** The campfire and the resting ring round it (idle heroes). */
export const FIRE = [1598, 352];
export const REST = [[1555, 362], [1640, 372], [1600, 330], [1530, 342], [1680, 350], [1570, 388], [1628, 394], [1665, 328], [1505, 372], [1712, 380]];

/** The dirt road along the front of the village (screen feet points), walked between spots. */
export const ROAD = [
  [0, 330], [200, 320], [420, 372], [640, 345], [900, 330], [1060, 360], [1350, 380], [1500, 372],
  [1610, 392], [1720, 360], [1900, 350], [2150, 372], [2330, 340], [2560, 330], [2700, 300],
];

/** Building hit boxes [x, y, w, h] and names (in the current language). */
export const BUILDINGS = {
  grange: { get nom() { return t("building").grange; }, box: [70, 75, 220, 200] },
  tour: { get nom() { return t("building").tour; }, box: [505, 15, 110, 350] },
  atelier: { get nom() { return t("building").atelier; }, box: [750, 70, 230, 200] },
  hotel: { get nom() { return t("building").hotel; }, box: [1115, 20, 330, 320] },
  taverne: { get nom() { return t("building").taverne; }, box: [1475, 40, 230, 250] },
  temple: { get nom() { return t("building").temple; }, box: [1890, 10, 270, 350] },
  caserne: { get nom() { return t("building").caserne; }, box: [2330, 100, 240, 220] },
};
/** The market's name in the current language. */
export const market = () => t("market");

/** Chimney tops (smoke) and live glows [x, y, size, color, period] (night only unless always). */
export const SMOKE = [[827, 95], [1590, 47], [1639, 100]];
export const GLOWS = [
  { at: [1598, 345], size: 120, color: "#ffa12b", period: 700, always: true }, // campfire
  { at: [815, 236], size: 60, color: "#ff8a2b", period: 650 }, // forge hearth
  { at: [558, 160], size: 70, color: "#a86bff", period: 2200 }, // tower windows
];
