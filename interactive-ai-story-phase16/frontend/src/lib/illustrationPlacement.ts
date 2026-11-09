export function illustrationLayout(generation: { width?: number; height?: number; imageCount?: number }) {
  const width = generation.width ?? 768
  const height = generation.height ?? 768
  const validResolution = (width === 768 && height === 768) || (width === 512 && height === 768) || (width === 768 && height === 512)
  const count = generation.imageCount ?? 2
  return {
    aspectRatio: validResolution ? `${width} / ${height}` : '768 / 768',
    orientation: validResolution && width !== height ? width > height ? 'landscape' : 'portrait' : 'square',
    count: Number.isInteger(count) && count >= 1 && count <= 4 ? count : 2,
  }
}

export function paragraphCanBeIllustrated(text: string): boolean {
  const value = text.trim()
  const runeCount = Array.from(value).length
  const wordCount = value.split(/\s+/u).filter(Boolean).length
  if (runeCount < 90 || wordCount < 14) return false
  const startsWithDialogue = /^(?:—|-|«|")/u.test(value)
  if (startsWithDialogue) return false
  return true
}
