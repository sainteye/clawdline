import type { TowerZone } from "./tower-state"

type Point = readonly [number, number]
export interface TowerScene {
  width: number
  height: number
  /** Feet positions in the background image's own pixels. */
  zones: Record<TowerZone, readonly Point[]>
}

/** V2, the neon ramen alley (ramen.webp, 1672×941). */
export const ramenScene: TowerScene = {
  width: 1672,
  height: 941,
  zones: {
    desk: [[655, 470], [1031, 470], [599, 680], [989, 692]],
    queue: [[720, 600], [800, 612], [880, 624]],
    repair: [[250, 700], [330, 722]],
    snack: [[146, 542], [209, 520], [279, 494], [341, 454]],
    lounge: [[780, 560], [1226, 532], [430, 790], [864, 812], [790, 372], [1460, 565]],
    bed: [[1252, 338], [1322, 352], [1210, 432], [1320, 452]],
  },
}
