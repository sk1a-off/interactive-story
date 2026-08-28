import { describe, expect, test } from 'vitest'
import { pendingTurnIsVisible, readerHasCoherentChoices } from '../src/lib/readerFlow'

describe('reader turn coherence', () => {
  test('never accepts choices that belong to the previous beat', () => {
    const current = {
      beatId: 'beat-new',
      headEventSeq: 12,
      choiceSet: { beatId: 'beat-old', choices: ['1', '2', '3', '4'] },
    }
    expect(readerHasCoherentChoices(current)).toBe(false)
    expect(pendingTurnIsVisible(current, { fromBeatId: 'beat-old', fromHeadEventSeq: 9 })).toBe(false)
  })

  test('keeps pending state until beat, head and choices advance together', () => {
    const pending = { fromBeatId: 'beat-old', fromHeadEventSeq: 9 }
    expect(pendingTurnIsVisible({
      beatId: 'beat-new',
      headEventSeq: 12,
      choiceSet: undefined,
    }, pending)).toBe(false)
    expect(pendingTurnIsVisible({
      beatId: 'beat-new',
      headEventSeq: 12,
      choiceSet: { beatId: 'beat-new', choices: ['1', '2', '3', '4'] },
    }, pending)).toBe(true)
  })
})
