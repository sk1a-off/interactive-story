import type { SetupComponent } from '../lib/api'

export type SetupPayload = Record<string, unknown>

export const SETUP_COMPONENT_ORDER = ['story_bible', 'player', 'world', 'world_rules', 'initial_cast', 'visual_bible', 'initial_quests', 'opening_situation'] as const

const TITLES: Record<string, string> = {
  story_bible: 'Основа истории',
  player: 'Главный герой',
  world: 'Мир',
  world_rules: 'Законы мира',
  initial_cast: 'Персонажи',
  visual_bible: 'Визуальный стиль',
  initial_quests: 'Стартовые квесты',
  opening_situation: 'Начальная сцена',
}

const DESCRIPTIONS: Record<string, string> = {
  story_bible: 'Главная идея, тон, темы и правила повествования.',
  player: 'Кто является главным героем, чего он хочет и как выглядит.',
  world: 'Состояние мира, важные места и визуальные ориентиры.',
  world_rules: 'Системы магии, технологий и других сил: законы, цены, пределы и исключения.',
  initial_cast: 'Персонажи, которые присутствуют в истории с самого начала.',
  visual_bible: 'Единый художественный язык для всех иллюстраций.',
  initial_quests: 'Главные и побочные квесты с их стартовыми этапами.',
  opening_situation: 'Первый эпизод, цель главы и четыре стартовых действия.',
}

export function setupComponentOrderIndex(key: string) {
  const index = SETUP_COMPONENT_ORDER.indexOf(key as typeof SETUP_COMPONENT_ORDER[number])
  return index < 0 ? SETUP_COMPONENT_ORDER.length : index
}

export function setupComponentTitle(key: string) {
  return TITLES[key] ?? key
}

export function setupComponentDescription(key: string) {
  return DESCRIPTIONS[key] ?? ''
}

export function setupDraftStorageKey(storyId: string, component: SetupComponent) {
  return `story-setup-draft:${storyId}:${component.key}:rev-${component.revision}`
}

export function readSetupDraftPayload(storyId: string, component: SetupComponent): SetupPayload {
  try {
    const raw = sessionStorage.getItem(setupDraftStorageKey(storyId, component))
    if (!raw) return { ...component.payload }
    const parsed = JSON.parse(raw) as unknown
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) return parsed as SetupPayload
  } catch {
    // A broken browser draft must never prevent opening the authoritative setup.
  }
  return { ...component.payload }
}

export function writeSetupDraftPayload(storyId: string, component: SetupComponent, payload: SetupPayload) {
  try { sessionStorage.setItem(setupDraftStorageKey(storyId, component), JSON.stringify(payload)) } catch { /* storage is only a UX fallback */ }
}

export function clearSetupDraftPayload(storyId: string, component: SetupComponent) {
  try { sessionStorage.removeItem(setupDraftStorageKey(storyId, component)) } catch { /* ignore unavailable storage */ }
}
