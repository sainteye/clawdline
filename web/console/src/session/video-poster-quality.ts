/** Rank actual decoded pixels; near-solid intros and fades have almost no detail. */
export function videoPosterScore(pixels: Uint8ClampedArray, width: number, height: number): number {
  const histogram = new Map<number, number>()
  let count = 0, sum = 0, squares = 0, edges = 0
  const left = Math.floor(width / 10), top = Math.floor(height / 10)
  const luminance = (i: number) => (pixels[i] * 0.299 + pixels[i + 1] * 0.587 + pixels[i + 2] * 0.114)
  for (let y = top; y < height - top; y++) for (let x = left; x < width - left; x++) {
    const i = (y * width + x) * 4, light = luminance(i)
    count++; sum += light; squares += light * light
    if (x > left) edges += Math.abs(light - luminance(i - 4))
    if (y > top) edges += Math.abs(light - luminance(i - width * 4))
    const color = (pixels[i] >> 4) * 256 + (pixels[i + 1] >> 4) * 16 + (pixels[i + 2] >> 4)
    histogram.set(color, (histogram.get(color) ?? 0) + 1)
  }
  if (!count) return 0
  const contrast = Math.sqrt(Math.max(0, squares / count - (sum / count) ** 2))
  const detail = edges / count
  if (contrast < 3 && detail < 2) return 0
  let dominant = 0
  for (const size of histogram.values()) dominant = Math.max(dominant, size)
  return contrast + detail * 2 + 64 * (1 - dominant / count)
}
