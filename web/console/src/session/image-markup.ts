export interface MarkPoint {
  x: number
  y: number
}

export interface MarkStroke {
  points: MarkPoint[]
}

export interface CanvasBounds {
  left: number
  top: number
  width: number
  height: number
}

export interface MarkupView {
  scale: number
  x: number
  y: number
}

export interface MarkupLayout extends CanvasBounds {
  fitWidth: number
  fitHeight: number
  maxScale: number
}

/** A pointer on the displayed canvas, expressed in the image's own pixels. */
export function imagePoint(
  clientX: number,
  clientY: number,
  bounds: CanvasBounds,
  imageWidth: number,
  imageHeight: number,
): MarkPoint {
  const x = bounds.width ? ((clientX - bounds.left) / bounds.width) * imageWidth : 0
  const y = bounds.height ? ((clientY - bounds.top) / bounds.height) * imageHeight : 0
  return {
    x: Math.max(0, Math.min(imageWidth, x)),
    y: Math.max(0, Math.min(imageHeight, y)),
  }
}

/** A red pen that stays the same visual weight on portrait and landscape images. */
export function markWidth(imageWidth: number, imageHeight: number): number {
  return Math.max(6, Math.min(24, Math.min(imageWidth, imageHeight) * 0.012))
}

/** The image's complete, aspect-preserving size inside the editor stage. */
export function fitImage(imageWidth: number, imageHeight: number, availableWidth: number, availableHeight: number) {
  if (imageWidth <= 0 || imageHeight <= 0 || availableWidth <= 0 || availableHeight <= 0) {
    return { width: 0, height: 0, maxScale: 4 }
  }
  const scale = Math.min(1, availableWidth / imageWidth, availableHeight / imageHeight)
  const width = imageWidth * scale
  const height = imageHeight * scale
  const pixelScale = Math.max(imageWidth / width, imageHeight / height)
  return { width, height, maxScale: Math.min(8, Math.max(4, pixelScale)) }
}

function viewLimits(scale: number, layout: MarkupLayout) {
  return {
    x: Math.max(0, (layout.fitWidth * scale - layout.width) / 2),
    y: Math.max(0, (layout.fitHeight * scale - layout.height) / 2),
  }
}

export function clampView(view: MarkupView, layout: MarkupLayout): MarkupView {
  const scale = Math.min(layout.maxScale, Math.max(1, view.scale))
  const limits = viewLimits(scale, layout)
  return {
    scale,
    x: Math.min(limits.x, Math.max(-limits.x, view.x)),
    y: Math.min(limits.y, Math.max(-limits.y, view.y)),
  }
}

/** Move the enlarged image without ever losing an edge beyond the stage. */
export function panView(view: MarkupView, dx: number, dy: number, layout: MarkupLayout): MarkupView {
  return clampView({ ...view, x: view.x + dx, y: view.y + dy }, layout)
}

/** Change scale while keeping the image point under the gesture's anchor in place. */
export function zoomView(
  view: MarkupView,
  scale: number,
  anchorX: number,
  anchorY: number,
  layout: MarkupLayout,
): MarkupView {
  const target = Math.min(layout.maxScale, Math.max(1, scale))
  if (target === 1) return { scale: 1, x: 0, y: 0 }
  const ratio = target / view.scale
  const middleX = layout.left + layout.width / 2
  const middleY = layout.top + layout.height / 2
  return clampView(
    {
      scale: target,
      x: (anchorX - middleX) * (1 - ratio) + view.x * ratio,
      y: (anchorY - middleY) * (1 - ratio) + view.y * ratio,
    },
    layout,
  )
}
