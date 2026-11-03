import { useEffect, useRef, useState } from "react"
import type Sigma from "sigma"
import type { GraphifyLink } from "@clawdline/contract"
import type { Positioned } from "./layout.js"
import { graphWord as w } from "./graphify.js"

export const GRAPH_COLORS = ["#eaa27d", "#88b9d9", "#a4c99b", "#b8a0d3", "#d9bd7d", "#78c1ba", "#d899b0", "#afb6c7"]

/** The canvas owns only a local view. All source text stays inert graph attributes. */
export default function SigmaGraph({ nodes, links, visible, selected, onSelect }: {
  nodes: Positioned[]; links: GraphifyLink[]; visible: Set<string>; selected: string; onSelect: (id: string) => void
}) {
  const container = useRef<HTMLDivElement>(null)
  const renderer = useRef<Sigma | null>(null)
  const current = useRef({ visible, selected, onSelect })
  current.current = { visible, selected, onSelect }
  const repaint = useRef<() => void>(() => {})
  const [state, setState] = useState("loading")
  const [zoom, setZoom] = useState(1)
  useEffect(() => {
    const element = container.current
    if (!element) return
    let disposed = false
    let active = ""
    let neighbors = new Set<string>()
    let hovered = ""
    const update = () => {
      active = current.current.selected || hovered
      neighbors = new Set([active])
      for (const link of links) if (link.source === active || link.target === active) {
        neighbors.add(link.source); neighbors.add(link.target)
      }
      renderer.current?.refresh()
    }
    repaint.current = update
    setState("loading")
    // Bundle both libraries locally, but load them only when there is a graph to view.
    void (async () => {
      try {
        const [{ default: SigmaRenderer, DEFAULT_STYLES }, { default: Graph }] = await Promise.all([import("sigma"), import("graphology")])
        if (disposed) return
        const graph = new Graph({ type: "directed", multi: true })
        const degree = new Map<string, number>()
        for (const link of links) {
          degree.set(link.source, (degree.get(link.source) ?? 0) + 1)
          degree.set(link.target, (degree.get(link.target) ?? 0) + 1)
        }
        for (const node of nodes) graph.addNode(node.id, {
          x: node.x, y: -node.y, label: node.label, color: GRAPH_COLORS[node.color], size: 3 + Math.log1p(degree.get(node.id) ?? 0),
        })
        links.forEach((link, i) => graph.addEdgeWithKey(String(i), link.source, link.target, { color: "#6e8194", size: .6 }))
        const instance = new SigmaRenderer(graph, element, {
          settings: { minCameraRatio: 1 / 4, maxCameraRatio: 1, enableCameraRotation: false, gestureTarget: "graph" },
          styles: {
            nodes: [DEFAULT_STYLES.nodes, { labelColor: "#dce2e8", labelSize: 12, labelFont: "system-ui, sans-serif", backdropColor: "#20252d", backdropShadowBlur: 0, backdropPadding: 5 }],
          },
          nodeReducer: (id, data) => {
            const emphasized = !!active && neighbors.has(id)
            return { visibility: current.current.visible.has(id) ? "visible" : "hidden", opacity: active && !emphasized ? .14 : 1,
              labelVisibility: active === id ? "visible" : active && !emphasized ? "hidden" : data.labelVisibility,
              color: active === id ? "#ffd3b9" : data.color, size: active === id ? data.size + 2 : data.size,
              zIndex: emphasized ? 1 : 0, backdropVisibility: active === id ? "visible" : data.backdropVisibility }
          },
          edgeReducer: (id) => {
            const link = links[Number(id)]
            const related = !!active && (link.source === active || link.target === active)
            return { visibility: current.current.visible.has(link.source) && current.current.visible.has(link.target) ? "visible" : "hidden",
              opacity: active ? related ? .9 : .04 : .22, color: related ? "#eaa27d" : "#6e8194", size: related ? 1.4 : .6 }
          },
        })
        renderer.current = instance
        instance.on("clickNode", ({ node }) => current.current.onSelect(node))
        instance.on("clickStage", () => current.current.onSelect(""))
        instance.on("enterNode", ({ node }) => { hovered = node; update() })
        instance.on("leaveNode", () => { hovered = ""; update() })
        instance.getCamera().on("updated", ({ ratio }) => { if (!disposed) setZoom(1 / ratio) })
        update(); setState("ready")
      } catch {
        if (!disposed) {
          renderer.current?.kill(); renderer.current = null
          element.replaceChildren(); setState("unavailable")
        }
      }
    })()
    const contextLost = (event: Event) => {
      event.preventDefault()
      renderer.current?.kill(); renderer.current = null
      if (!disposed) setState("unavailable")
    }
    element.addEventListener("webglcontextlost", contextLost, true)
    return () => {
      disposed = true; repaint.current = () => {}
      element.removeEventListener("webglcontextlost", contextLost, true)
      renderer.current?.kill(); renderer.current = null
    }
  }, [nodes, links])
  useEffect(() => { repaint.current() }, [visible, selected])
  const changeZoom = (factor: number) => {
    const camera = renderer.current?.getCamera()
    if (camera) camera.setState({ ratio: camera.getBoundedRatio(camera.ratio * factor) })
  }
  const fit = () => renderer.current?.getCamera().setState({ x: .5, y: .5, angle: 0, ratio: 1 })
  const focus = () => {
    const instance = renderer.current
    const point = instance?.getNodeDisplayData(selected)
    if (point) instance?.getCamera().setState({ x: point.x, y: point.y })
  }
  return <div className="graphify-stage" data-renderer-state={state}>
    <div ref={container} className="graphify-canvas" tabIndex={0} role="region" aria-label={w("canvas")} aria-describedby="graphify-gesture-hint"
      onKeyDown={event => {
        const camera = renderer.current?.getCamera()
        if (!camera) return
        if (["+", "=", "-", "0", "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight"].includes(event.key)) event.preventDefault()
        if (event.key === "+" || event.key === "=") changeZoom(1 / 1.5)
        if (event.key === "-") changeZoom(1.5)
        if (event.key === "0") fit()
        if (event.key === "ArrowLeft") camera.setState({ x: camera.x - .1 * camera.ratio })
        if (event.key === "ArrowRight") camera.setState({ x: camera.x + .1 * camera.ratio })
        if (event.key === "ArrowUp") camera.setState({ y: camera.y + .1 * camera.ratio })
        if (event.key === "ArrowDown") camera.setState({ y: camera.y - .1 * camera.ratio })
      }} />
    {state !== "ready" && <p className="graphify-render-message" role="status">{w(state === "unavailable" ? "rendererUnavailable" : "loading")}</p>}
    {state === "ready" && selected && <button className="graphify-focus" type="button" onClick={focus}>{w("focusNode")}</button>}
    {state === "ready" && <div className="graphify-zoom">
      <button type="button" aria-label={w("zoomOut")} disabled={zoom <= 1} onClick={() => changeZoom(1.5)}>−</button>
      <button type="button" onClick={fit} title={w("fit")} aria-label={w("fit")}>{Math.round(zoom * 100)}%</button>
      <button type="button" aria-label={w("zoomIn")} disabled={zoom >= 4} onClick={() => changeZoom(1 / 1.5)}>+</button>
    </div>}
    <p className="graphify-gesture" id="graphify-gesture-hint">{w("gestures")}</p>
  </div>
}
