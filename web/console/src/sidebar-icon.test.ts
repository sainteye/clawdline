// The drawer's settings mark, measured rather than looked at:
// `node --test web/console/src/sidebar-icon.test.ts`.
//
// A gear reads as a gear because every tooth is the same distance from the
// hub it turns on. The first drawing was laid out around (12, 9.47) and then
// pulled at with `transform="translate(0 1.68) scale(1 .86)"`, which left the
// teeth 7.70 wide but 6.62 tall and the body 2.18 above the hub: on screen the
// gear looked tipped over. tsc and the build both pass on that, so the shape
// is checked here, out of the source, as numbers.
import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"

const source = readFileSync(new URL("./SidebarIcon.tsx", import.meta.url), "utf8")

/** The `d` of the one `<path>` in the block that draws `name`. */
function pathOf(name: string): string {
  const block = source.split(`{name === "${name}" && (`)[1]
  assert.ok(block, `no block draws ${name}`)
  const d = /<path d="([^"]+)"/.exec(block)
  assert.ok(d, `${name} has no path`)
  return d[1]
}

/** The `<circle>` of the block that draws `name`, as centre and radius. */
function circleOf(name: string): { cx: number; cy: number; r: number } {
  const block = source.split(`{name === "${name}" && (`)[1]
  const c = /<circle cx="([\d.]+)" cy="([\d.]+)" r="([\d.]+)"/.exec(block)
  assert.ok(c, `${name} has no circle`)
  return { cx: Number(c[1]), cy: Number(c[2]), r: Number(c[3]) }
}

/** Every corner a straight-line path passes through, in user units. */
function corners(d: string): { x: number; y: number }[] {
  const tokens = d.match(/[MmLlHhVvZz]|-?\d*\.?\d+/g) ?? []
  const points: { x: number; y: number }[] = []
  let x = 0, y = 0, command = ""
  for (let i = 0; i < tokens.length;) {
    const token = tokens[i]
    if (/[A-Za-z]/.test(token)) {
      command = token
      i += 1
      if (command === "Z" || command === "z") break
      continue
    }
    const take = () => Number(tokens[i++])
    switch (command) {
      case "M": x = take(); y = take(); command = "L"; break
      case "m": x += take(); y += take(); command = "l"; break
      case "L": x = take(); y = take(); break
      case "l": x += take(); y += take(); break
      case "H": x = take(); break
      case "h": x += take(); break
      case "V": y = take(); break
      case "v": y += take(); break
      default: assert.fail(`curves are not measured here: ${command}`)
    }
    points.push({ x, y })
  }
  return points
}

test("the settings gear turns around its own hub", () => {
  const gear = corners(pathOf("settings"))
  const hub = circleOf("settings")
  assert.equal(gear.length, 24, "six teeth, four corners each")
  const centre = {
    x: gear.reduce((sum, p) => sum + p.x, 0) / gear.length,
    y: gear.reduce((sum, p) => sum + p.y, 0) / gear.length,
  }
  assert.ok(Math.abs(centre.x - hub.cx) < 0.05, `gear centre x ${centre.x} is not the hub's ${hub.cx}`)
  assert.ok(Math.abs(centre.y - hub.cy) < 0.05, `gear centre y ${centre.y} is not the hub's ${hub.cy}`)
})

test("every tooth stands at one of two radii, so none of them leans", () => {
  const hub = circleOf("settings")
  const radii = corners(pathOf("settings")).map((p) => Math.hypot(p.x - hub.cx, p.y - hub.cy))
  const tip = Math.max(...radii)
  const root = Math.min(...radii)
  for (const r of radii) {
    assert.ok(
      Math.abs(r - tip) < 0.02 || Math.abs(r - root) < 0.02,
      `a corner sits at ${r.toFixed(2)}, neither the tip ${tip.toFixed(2)} nor the root ${root.toFixed(2)}`,
    )
  }
  assert.ok(root > hub.r + 1, `the teeth at ${root.toFixed(2)} would touch a hub of ${hub.r}`)
  assert.ok(tip < 12 - 1, `the teeth at ${tip.toFixed(2)} would leave the 24-unit box`)
})

test("no mark in the drawer is stretched along one axis only", () => {
  for (const [, transform] of source.matchAll(/transform="([^"]*)"/g)) {
    const scale = /scale\(\s*([\d.]+)[\s,]+([\d.]+)\s*\)/.exec(transform)
    if (!scale) continue
    assert.equal(scale[1], scale[2], `scale(${scale[1]} ${scale[2]}) squashes a mark out of round`)
  }
})
