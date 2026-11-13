import { describe, expect, test } from 'vitest'
import { applyGenerationUpdate, pendingTurnIsVisible, provisionalVisible, readerHasCoherentChoices } from '../src/lib/readerFlow'

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

describe('streaming draft snapshots',()=>{
 const pending={generationId:'g',fromBeatId:'old',fromHeadEventSeq:1,phase:'writing',createdAt:1}
 test('replaces cumulative text and accepts an explicit reset',()=>{
  const first=applyGenerationUpdate('t',pending,{phase:'draft_streaming',generationId:'g',provisional:true,revision:1000001,textDelta:'Начало'})
  const second=applyGenerationUpdate('t',first,{phase:'draft_streaming',provisional:true,revision:1000002,textDelta:'Начало и конец'})
  expect(second?.provisionalText).toBe('Начало и конец')
  const reset=applyGenerationUpdate('t',second,{phase:'draft_streaming',provisional:true,revision:1000003})
  expect(reset?.provisionalText).toBeUndefined()
  const ready=applyGenerationUpdate('t',reset,{phase:'draft_replaced',provisional:true,revision:1000004,textDelta:'Исправленный итог'})
  expect(ready?.provisionalText).toBe('Исправленный итог')
 })
 test('retry clears old text and prevents delayed snapshots from returning',()=>{
  const first=applyGenerationUpdate('t',pending,{phase:'draft_streaming',provisional:true,revision:1000004,textDelta:'Старый'})
  const retry=applyGenerationUpdate('t',first,{phase:'retrying',revision:1999999})
  expect(retry?.provisionalText).toBeUndefined()
  expect(applyGenerationUpdate('t',retry,{phase:'draft_streaming',provisional:true,revision:1000005,textDelta:'Запоздавший'})).toBe(retry)
  const next=applyGenerationUpdate('t',retry,{phase:'draft_streaming',provisional:true,revision:2000001,textDelta:'Новый'})
  expect(next?.provisionalText).toBe('Новый')
 })
})

describe('provisionalVisible', () => {
  test('shows the draft only while the source beat is still the current one', () => {
    const pending = { provisionalText: 'Черновик', fromBeatId: 'beat-1' }
    expect(provisionalVisible({ beatId: 'beat-1' }, pending)).toBe(true)
    expect(provisionalVisible({ beatId: 'beat-2' }, pending)).toBe(false)
    expect(provisionalVisible({ beatId: 'beat-1' }, { provisionalText: undefined, fromBeatId: 'beat-1' })).toBe(false)
    expect(provisionalVisible({ beatId: 'beat-1' }, null)).toBe(false)
  })
})
