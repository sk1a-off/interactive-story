export function paragraphCanBeIllustrated(text: string): boolean {
  const value = text.trim()
  const runeCount = Array.from(value).length
  const wordCount = value.split(/\s+/u).filter(Boolean).length
  if (runeCount < 90 || wordCount < 14) return false
  const startsWithDialogue = /^(?:—|-|«|")/u.test(value)
  if (startsWithDialogue) return false
  return true
}
