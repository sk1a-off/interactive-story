import { describe, expect, test } from 'vitest'
import { applyGenerationUpdate, pendingTurnIsVisible, readerHasCoherentChoices } from '../src/lib/readerFlow'

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

describe('provisional reader updates', () => {
  const pending = { generationId: 'generation-a', requestId: 'request-a', actionText: 'Идти', fromBeatId: 'beat-old', fromHeadEventSeq: 9, phase: 'writing', createdAt: 1 }

  test('accepts only matching timeline and generation and keeps newest revision', () => {
    const draft = applyGenerationUpdate('timeline-a', pending, { generationId: 'generation-a', timelineId: 'timeline-a', phase: 'draft_ready', revision: 1, provisional: true, textDelta: 'Новый текст' })
    expect(draft?.provisionalText).toBe('Новый текст')
    expect(applyGenerationUpdate('timeline-a', draft, { generationId: 'generation-a', timelineId: 'timeline-a', phase: 'draft_replaced', revision: 0, provisional: true, textDelta: 'Старый текст' })).toEqual(draft)
    expect(applyGenerationUpdate('timeline-b', pending, { generationId: 'generation-a', timelineId: 'timeline-a', phase: 'draft_ready', revision: 1, provisional: true, textDelta: 'Утечка' })).toEqual(pending)
    expect(applyGenerationUpdate('timeline-a', pending, { generationId: 'generation-b', timelineId: 'timeline-a', phase: 'draft_ready', revision: 1, provisional: true, textDelta: 'Утечка' })).toEqual(pending)
  })

  test('retains draft while finalizing so committed snapshot can replace it atomically', () => {
    const draft = { ...pending, provisionalText: 'Новый текст', provisionalRevision: 1 }
    expect(applyGenerationUpdate('timeline-a', draft, { generationId: 'generation-a', phase: 'finalizing' })).toMatchObject({ phase: 'finalizing', provisionalText: 'Новый текст' })
  })
})
