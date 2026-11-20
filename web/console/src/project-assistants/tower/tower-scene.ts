import type { TowerZone } from "./tower-state"

type Point = readonly [number, number]
export interface TowerScene {
  /** Which picture; BotTower maps it to the image file. */
  picture: "skyline" | "rain" | "ramen" | "platform"
  width: number
  height: number
  /** Feet positions in the background image's own pixels. */
  zones: Record<TowerZone, readonly Point[]>
}

/** V2, the neon ramen alley (ramen.webp, 1672×941). */
export const ramenScene: TowerScene = {
  picture: "ramen",
  width: 1672,
  height: 941,
  zones: {
    desk: [[655, 470], [1031, 470], [599, 680], [989, 692]],
    queue: [[720, 600], [800, 612], [880, 624]],
    repair: [[250, 700], [330, 722]],
    // Strolling spots, including the ramen bar stools.
    lounge: [[780, 560], [209, 520], [1226, 532], [430, 790], [341, 454], [864, 812], [790, 372], [146, 542], [1460, 565]],
    bed: [[1252, 338], [1322, 352], [1210, 432], [1320, 452]],
  },
}

/** V3, the skyline penthouse (skyline.webp). */
export const skylineScene: TowerScene = {
  picture: "skyline",
  width: 1672,
  height: 941,
  zones: {
    desk: [[606, 420], [947, 420], [606, 622], [947, 636]],
    queue: [[697, 545], [780, 518], [864, 538]],
    repair: [[279, 672], [376, 700]],
    lounge: [[209, 572], [780, 728], [265, 538], [1115, 657], [313, 516], [1184, 462], [362, 490], [780, 336]],
    bed: [[1226, 334], [1292, 348], [1200, 424], [1292, 442]],
  },
}

/** V1, the rainy neon office (rain.webp); three desks. */
export const rainScene: TowerScene = {
  picture: "rain",
  width: 1672,
  height: 941,
  zones: {
    desk: [[515, 615], [794, 573], [1059, 531]],
    queue: [[864, 700], [975, 686], [1087, 672]],
    repair: [[265, 700], [181, 728]],
    lounge: [[139, 838], [697, 866], [1184, 657], [320, 838], [1324, 727], [1003, 782]],
    bed: [[1282, 448], [1365, 462], [1254, 560], [1379, 574]],
  },
}

/** B1, the underground platform (platform.webp). */
export const platformScene: TowerScene = {
  picture: "platform",
  width: 1672,
  height: 941,
  zones: {
    desk: [[655, 448], [1010, 441], [711, 699], [1087, 685]],
    queue: [[864, 559], [975, 573], [780, 587]],
    repair: [[418, 727], [334, 713]],
    lounge: [[265, 629], [836, 782], [320, 615], [1226, 531], [376, 601], [1254, 727]],
    bed: [[1463, 448], [1519, 462], [1338, 587], [1449, 601]],
  },
}

/** Floors from the top: 4F, 3F, 2F, B1. Every Project gets one in a stable
 * order; a fifth Project shares from the top again. */
export const towerFloors = [
  { label: "4F", scene: skylineScene },
  { label: "3F", scene: rainScene },
  { label: "2F", scene: ramenScene },
  { label: "B1", scene: platformScene },
] as const
