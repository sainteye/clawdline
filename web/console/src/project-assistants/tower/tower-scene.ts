import type { TowerZone } from "./tower-state"

type Point = readonly [number, number]
export interface TowerScene {
  /** Which picture; BotTower maps it to the image file. */
  picture: "skyline" | "rain" | "ramen" | "platform" | "building"
  width: number
  height: number
  /** Feet positions in the background image's own pixels. */
  zones: Record<TowerZone, readonly Point[]>
  /** The centre of the floor's portal pad, where a Bot's helpers appear. */
  portal: Point
}

/** V2, the neon ramen alley (ramen.webp, 1672×941). */
export const ramenScene: TowerScene = {
  picture: "ramen",
  width: 1672,
  height: 941,
  zones: {
    desk: [[655, 470], [1031, 470], [599, 680], [989, 692]],
    // The aisle between the desk rows, not on a desk.
    queue: [[700, 515], [770, 520], [840, 525]],
    repair: [[250, 700], [330, 722]],
    // Bar stools, the open floor and the portal.
    lounge: [[780, 372], [146, 542], [1226, 532], [209, 520], [560, 780], [279, 494], [864, 812], [341, 454], [1460, 565]],
    bed: [[1252, 338], [1322, 352], [1210, 432], [1250, 462]],
  },
  portal: [1410, 680],
}

/** V3, the skyline penthouse (skyline.webp). */
export const skylineScene: TowerScene = {
  picture: "skyline",
  width: 1672,
  height: 941,
  zones: {
    desk: [[606, 420], [947, 420], [606, 622], [947, 636]],
    // The centre aisle between the desk pairs.
    queue: [[780, 450], [780, 520], [780, 590]],
    repair: [[279, 672], [376, 700]],
    // Bar stools, the rug, the open floor.
    lounge: [[209, 572], [780, 728], [265, 538], [1115, 657], [313, 516], [1184, 462], [362, 490], [780, 336]],
    bed: [[1226, 334], [1292, 348], [1200, 424], [1292, 442]],
  },
  // The drone pad on the balcony.
  portal: [1520, 572],
}

/** V1, the rainy neon office (rain.webp); three desks. */
export const rainScene: TowerScene = {
  picture: "rain",
  width: 1672,
  height: 941,
  zones: {
    desk: [[520, 590], [800, 560], [1062, 522]],
    queue: [[870, 700], [975, 686], [1087, 672]],
    repair: [[255, 690], [330, 668]],
    // The sofa, the open floor by the windows and the portal.
    lounge: [[210, 800], [1180, 650], [310, 790], [980, 640], [1300, 705], [860, 770]],
    bed: [[1282, 448], [1365, 462], [1255, 560], [1380, 574]],
  },
  portal: [1480, 628],
}

/** B1, the underground platform (platform.webp). */
export const platformScene: TowerScene = {
  picture: "platform",
  width: 1672,
  height: 941,
  zones: {
    desk: [[655, 448], [1010, 441], [711, 699], [1087, 685]],
    // The aisle between the two desk rows.
    queue: [[740, 475], [810, 480], [880, 485]],
    repair: [[418, 727], [334, 713]],
    // Noodle-bar stools, the platform floor, the train door.
    lounge: [[265, 629], [836, 782], [320, 615], [1226, 531], [376, 601], [1254, 727]],
    bed: [[1463, 448], [1519, 462], [1338, 587], [1449, 601]],
  },
  // The lit pad inside the service lift.
  portal: [1180, 250],
}

/** Floors from the top: 4F, 3F, 2F, B1. Every Project gets one in a stable
 * order; a fifth Project shares from the top again. */
export const towerFloors = [
  { label: "4F", scene: skylineScene },
  { label: "3F", scene: rainScene },
  { label: "2F", scene: ramenScene },
  { label: "B1", scene: platformScene },
] as const

type Band = readonly [number, number]
/** The building cross-section (building.webp): each floor's strip, its busy
 * spots (desks, the queue, the failure corner), its rest spots, and the
 * elevator lamp that lights when someone on that floor waits. */
export interface TowerSectionFloor {
  band: Band
  desk: readonly Point[]
  queue: readonly Point[]
  repair: readonly Point[]
  rest: readonly Point[]
  lamp: Point
}
export const sectionScene = {
  picture: "building" as const,
  width: 1672,
  height: 941,
  /** Plates and frames span this many pixels from the shaft to the outer wall. */
  left: 345,
  right: 1570,
  floors: [
    { band: [60, 262], desk: [[580, 238], [750, 238], [905, 238]], queue: [[665, 248], [830, 248], [990, 248]], repair: [[420, 245]], rest: [[1150, 242], [1230, 242], [1440, 236], [1050, 245]], lamp: [230, 158] },
    { band: [270, 465], desk: [[585, 432], [750, 432], [905, 432], [1075, 432]], queue: [[665, 448], [830, 448], [990, 448]], repair: [[400, 445]], rest: [[1270, 440], [1325, 440], [470, 448], [1180, 448]], lamp: [230, 358] },
    { band: [480, 677], desk: [[590, 652], [755, 652], [910, 652]], queue: [[670, 660], [835, 660], [990, 660]], repair: [[420, 640]], rest: [[1085, 652], [1400, 618], [1455, 618], [1240, 655]], lamp: [230, 565] },
    { band: [700, 912], desk: [[710, 868], [875, 868], [1040, 868]], queue: [[790, 880], [955, 880], [1110, 880]], repair: [[580, 880]], rest: [[1300, 872], [1385, 872], [480, 878], [1200, 880]], lamp: [230, 787] },
  ] as readonly TowerSectionFloor[],
}
