import type { Current } from './api'

export type PendingReaderGeneration = {
  generationId?: string
  requestId?: string
  actionText?: string
  fromBeatId: string
  fromHeadEventSeq: number
  phase: string
  createdAt: number
  provisionalText?: string
  provisionalRevision?: number
}

export type GenerationUpdate = {
  generationId?: string
  timelineId?: string
  phase: string
  revision?: number
  provisional?: boolean
  textDelta?: string
  error?: string
}

const maxPendingAgeMs = 30 * 60 * 1000

export function readerHasCoherentChoices(current: Pick<Current, 'beatId' | 'choiceSet'>): boolean {
  return current.choiceSet?.beatId === current.beatId && current.choiceSet.choices.length === 4
}

export function pendingTurnIsVisible(
  current: Pick<Current, 'beatId' | 'headEventSeq' | 'choiceSet'>,
  pending: Pick<PendingReaderGeneration, 'fromBeatId' | 'fromHeadEventSeq'>,
): boolean {
  return current.beatId !== pending.fromBeatId
    && current.headEventSeq > pending.fromHeadEventSeq
    && readerHasCoherentChoices(current)
}

export function applyGenerationUpdate(
  timelineId: string,
  pending: PendingReaderGeneration | null,
  update: GenerationUpdate,
): PendingReaderGeneration | null {
  if (!pending || !pending.generationId) return pending
  if (update.generationId && update.generationId !== pending.generationId) return pending
  if (update.timelineId && update.timelineId !== timelineId) return pending
  if (update.phase === 'draft_ready' || update.phase === 'draft_replaced') {
    const revision = update.revision ?? 0
    if (!update.provisional || !update.textDelta?.trim() || revision < (pending.provisionalRevision ?? 0)) return pending
    return { ...pending, phase: update.phase, provisionalText: update.textDelta, provisionalRevision: revision }
  }
  return { ...pending, phase: update.phase }
}

export function readPendingReaderGeneration(timelineId: string): PendingReaderGeneration | null {
  if (typeof sessionStorage === 'undefined') return null
  const raw = sessionStorage.getItem(`reader-generation:${timelineId}`)
  if (!raw) return null
  try {
    const value = JSON.parse(raw) as PendingReaderGeneration
    if (!value.fromBeatId || !Number.isFinite(value.fromHeadEventSeq) || !Number.isFinite(value.createdAt)) {
      sessionStorage.removeItem(`reader-generation:${timelineId}`)
      return null
    }
    if (Date.now() - value.createdAt > maxPendingAgeMs) {
      sessionStorage.removeItem(`reader-generation:${timelineId}`)
      return null
    }
    return { ...value, phase: value.phase || 'syncing' }
  } catch {
    sessionStorage.removeItem(`reader-generation:${timelineId}`)
    return null
  }
}

export function writePendingReaderGeneration(timelineId: string, pending: PendingReaderGeneration | null): void {
  if (typeof sessionStorage === 'undefined') return
  const key = `reader-generation:${timelineId}`
  if (pending) sessionStorage.setItem(key, JSON.stringify(pending))
  else sessionStorage.removeItem(key)
}
