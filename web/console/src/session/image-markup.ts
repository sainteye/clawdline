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
