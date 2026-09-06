import { useEffect, useMemo, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { assistSetup, type SetupAssistChange, type SetupAssistOperation, type SetupAssistResult, type SetupAssistTarget, type SetupComponent } from '../lib/api'
import { readSetupDraftPayload, SETUP_COMPONENT_ORDER, setupComponentTitle, writeSetupDraftPayload } from './setupEditorModel'

type Scope = 'current' | 'all'
type RunRequest = { instruction: string; scope: Scope; action?: string; target?: SetupAssistTarget }
type NamedItem = { label: string; matchField: string; matchValue: string }

const SECTION_ACTIONS: Record<string, Array<{ label: string; instruction: string; action?: string }>> = {
  story_bible: [
    { label: '✨ Уточнить замысел', instruction: 'Уточни только необходимые поля основы истории: сделай центральный конфликт конкретнее, сохрани исходный замысел.' },
    { label: '🎭 Усилить темы', instruction: 'Проверь темы и тон. Добавь только недостающую смысловую глубину, не переписывая уже удачные поля.' },
  ],
  player: [
    { label: '✨ Углубить героя', instruction: 'Углуби личность, прошлое, сильные стороны и внутреннее противоречие героя. Сохрани имя, возраст, цели и установленные факты.', action: 'improve_player_profile' },
    { label: '🧭 Согласовать мотивацию', instruction: 'Согласуй описание героя и его цели: желания должны быть конкретными, допускать разные решения игрока и вытекать из прошлого героя. Не меняй имя, возраст и внешность.', action: 'improve_player_motivation' },
    { label: '🖼 Улучшить внешность', instruction: 'Улучши только visualAnchorEn героя: дай на английском устойчивые черты лица, телосложения, волос, одежды и заметные детали без действия, эмоции и фона.', action: 'improve_player_visual' },
  ],
  world: [
    { label: '🌍 Уточнить состояние мира', instruction: 'Уточни текущее состояние мира и только те детали, которые влияют на начало и дальнейший сюжет.' },
    { label: '🧩 Проверить связность мест', instruction: 'Проверь, согласованы ли важные локации с замыслом и начальной сценой. Предложи только адресные исправления.' },
  ],
  initial_cast: [
    { label: '🎭 Углубить роли', instruction: 'Сделай роли и отношения стартовых персонажей различимыми, не меняя их имена и установленные факты.', action: 'improve_cast_roles' },
    { label: '🖼 Проверить внешность', instruction: 'Проверь только visualAnchorEn персонажей на визуальную устойчивость и противоречия.', action: 'improve_cast_visuals' },
  ],
  visual_bible: [
    { label: '🎨 Собрать стиль', instruction: 'Сделай визуальный стиль цельным и применимым к серии иллюстраций. Меняй только слабые или противоречивые поля.', action: 'improve_visual_style' },
    { label: '🎥 Улучшить камеру', instruction: 'Уточни кинематографию без изменения содержания истории.', action: 'improve_visual_camera' },
  ],
  initial_quests: [
    { label: '🧭 Проверить структуру', instruction: 'Проверь главные и побочные квесты и их этапы: убери смысловые повторы, сделай условия достижения проверяемыми. Главными оставь только центральные сюжетные линии, необязательные поручения пометь questType side.' },
    { label: '🪜 Проверить этапы', instruction: 'Проверь, что каждый большой квест имеет несколько конкретных стартовых этапов, а новые этапы можно будет естественно добавлять по ходу истории.' },
  ],
  opening_situation: [
    { label: '🎬 Усилить зацепку', instruction: 'Улучши только слабые места вступления: убери повторы, усили зацепку и сохрани выбранный POV и установленные факты.', action: 'improve_opening_prose' },
    { label: '🧭 Улучшить варианты', instruction: 'Измени только четыре стартовых действия: они должны быть разными намерениями игрока, а не гарантированными результатами.', action: 'improve_opening_choices' },
  ],
}

function rows(value: unknown): Array<Record<string, unknown>> {
  return Array.isArray(value) ? value.map(item => item && typeof item === 'object' && !Array.isArray(item) ? item as Record<string, unknown> : { value: item }) : []
}

function namedItems(value: unknown, field: string): NamedItem[] {
  if (!Array.isArray(value)) return []
  return value.map((item, index) => {
    if (item && typeof item === 'object' && !Array.isArray(item)) {
      const label = String((item as Record<string, unknown>)[field] ?? `${index + 1}`)
      return { label, matchField: field, matchValue: label }
    }
    const label = String(item ?? '')
    return { label, matchField: '', matchValue: label }
  }).filter(item => item.label.trim())
}

function operationLabel(operation: SetupAssistOperation): string {
  const target = operation.childMatchValue || operation.matchValue
  const place = operation.childPath || operation.path
  const verbs: Record<string, string> = {
    set_field: 'Изменить поле', remove_field: 'Удалить поле', add_item: 'Добавить элемент', update_item: 'Изменить элемент', remove_item: 'Удалить элемент',
    add_child_item: 'Добавить вложенный этап', update_child_item: 'Изменить вложенный этап', remove_child_item: 'Удалить вложенный этап',
  }
  return `${verbs[operation.operation] ?? operation.operation}: ${target || place}`
}

function assistantErrorMessage(error: Error): string {
  const raw = error.message.trim()
  if (raw.includes('invalid proposal') || raw.includes('invalid setup assistant')) {
    return 'Модель вернула правку в неподходящем формате. Помощник уже повторил только этот запрос; попробуйте сформулировать изменение короче или выбрать конкретный элемент.'
  }
  if (raw.includes('provider unavailable')) return 'Локальная модель сейчас недоступна. Проверьте, что её сервер запущен, и повторите запрос.'
  return raw
}

function OperationRow({ operation }: { operation: SetupAssistOperation }) {
  return <div className={`setup-assistant-operation ${operation.operation.includes('remove') ? 'remove' : ''}`}>
    <strong>{operationLabel(operation)}</strong>
    {operation.matchValue && operation.childPath && <small>Внутри: {operation.matchValue}</small>}
    {operation.value !== undefined && <pre>{typeof operation.value === 'string' ? operation.value : JSON.stringify(operation.value, null, 2)}</pre>}
  </div>
}

export function SetupAssistant({ storyId, components, selectedKey, onSelectComponent, onDraftsApplied }: {
  storyId: string
  components: SetupComponent[]
  selectedKey: string
  onSelectComponent: (key: string) => void
  onDraftsApplied: () => void
}) {
  const [instruction, setInstruction] = useState('')
  const [proposal, setProposal] = useState<SetupAssistResult | null>(null)
  const [appliedKeys, setAppliedKeys] = useState<string[]>([])
  const [selectedItem, setSelectedItem] = useState<NamedItem | null>(null)
  const [selectedStage, setSelectedStage] = useState<NamedItem | null>(null)
  const activeComponents = useMemo(() => components.filter(component => SETUP_COMPONENT_ORDER.includes(component.key as typeof SETUP_COMPONENT_ORDER[number])), [components])
  const selected = activeComponents.find(component => component.key === selectedKey) ?? activeComponents[0]
  const editable = useMemo(() => activeComponents.filter(component => !component.locked), [activeComponents])
  const draft = selected ? readSetupDraftPayload(storyId, selected) : {}
  const quickActions = SECTION_ACTIONS[selected?.key] ?? []

  const mutation = useMutation({
    mutationFn: ({ instruction: nextInstruction, scope, action, target }: RunRequest) => {
      const requestKeys = scope === 'current' ? (selected && !selected.locked ? [selected.key] : []) : editable.map(component => component.key)
      if (requestKeys.length === 0) throw new Error('В выбранной области нет разделов, доступных для AI.')
      const drafts = Object.fromEntries(activeComponents.map(component => [component.key, readSetupDraftPayload(storyId, component)]))
      return assistSetup(storyId, { instruction: nextInstruction, components: requestKeys, drafts, action, target })
    },
    onSuccess: result => { setProposal(result); setAppliedKeys([]) },
  })

  useEffect(() => {
    setSelectedItem(null)
    setSelectedStage(null)
    setInstruction('')
    setProposal(null)
    setAppliedKeys([])
    mutation.reset()
  // Reset section-local assistant state when the editor switches sections.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected?.key])

  const run = (nextInstruction = instruction, scope: Scope = 'current', action = 'custom', target?: SetupAssistTarget) => {
    const clean = nextInstruction.trim()
    if (!clean || !selected || (selected.locked && scope === 'current')) return
    setInstruction(nextInstruction)
    setProposal(null)
    mutation.reset()
    mutation.mutate({ instruction: clean, scope, action, target })
  }
  const address = (action: string, target: SetupAssistTarget, fallback: string) => run(instruction.trim() || fallback, 'current', action, target)

  const applyChanges = (changes: SetupAssistChange[]) => {
    const applied: string[] = []
    for (const change of changes) {
      const component = components.find(candidate => candidate.key === change.key)
      if (!component || component.locked) continue
      writeSetupDraftPayload(storyId, component, change.after)
      applied.push(change.key)
    }
    if (!applied.length) return
    setAppliedKeys(current => Array.from(new Set([...current, ...applied])))
    onDraftsApplied()
    onSelectComponent(applied[0])
  }

  const collection = selected?.key === 'world' ? { path: 'locations', title: 'Локации', add: 'Добавить локацию', items: namedItems(draft.locations, 'name') }
    : selected?.key === 'initial_cast' ? { path: 'characters', title: 'Персонажи', add: 'Добавить персонажа', items: namedItems(draft.characters, 'name') }
    : selected?.key === 'story_bible' ? { path: 'themes', title: 'Темы', add: 'Добавить тему', items: namedItems(draft.themes, '') }
    : selected?.key === 'visual_bible' ? { path: 'continuityRules', title: 'Правила непрерывности', add: 'Добавить правило', items: namedItems(draft.continuityRules, '') }
    : selected?.key === 'opening_situation' ? { path: 'choices', title: 'Стартовые действия', add: '', items: namedItems(draft.choices, '') }
    : null
  const playerGoals = selected?.key === 'player' ? namedItems(draft.goals, '') : []
  const quests = selected?.key === 'initial_quests' ? namedItems(draft.quests, 'title') : []
  const selectedQuest = rows(draft.quests).find(quest => String(quest.title ?? '') === selectedItem?.matchValue)
  const stages = selectedQuest ? namedItems(selectedQuest.stages, 'title') : []
  const busy = mutation.isPending
  const locked = !selected || selected.locked

  return <aside className="setup-assistant-card" aria-label="AI помощник setup">
    <div className="setup-assistant-head"><div><p className="eyebrow">Помощник раздела</p><h2>{selected ? setupComponentTitle(selected.key) : 'AI-помощник'}</h2></div><span className="assistant-safe-chip">адресные правки</span></div>
    <p className="muted tiny">Помощник учитывает весь setup, но изменяет только открытый раздел. Коллекции редактируются отдельными операциями — без замены всего JSON.</p>
    {locked && <p className="setup-assistant-lock-note">🔒 Раздел зафиксирован. Разблокируйте его, чтобы AI мог предложить изменения.</p>}

    <div className="setup-assistant-quick">{quickActions.map(action => <button type="button" className="ghost compact" key={action.label} disabled={busy || locked} onClick={() => run(action.instruction, 'current', action.action ?? 'improve')}>{action.label}</button>)}</div>

    {selected?.key === 'player' && <>
      <section className="assistant-entities assistant-player-fields">
        <div className="assistant-entities-head"><div><strong>Что изменить у героя</strong><small>Выберите поле — AI не затронет остальные.</small></div></div>
        <div className="assistant-player-field-grid">
          <button type="button" disabled={busy || locked} onClick={() => address('set_field', { path: 'name' }, 'Предложи подходящее имя главного героя с учётом замысла и мира. Верни только имя.')}>Имя <small>{String(draft.name ?? 'не задано')}</small></button>
          <button type="button" disabled={busy || locked} onClick={() => address('set_field', { path: 'age' }, 'Подбери уместный совершеннолетний возраст главного героя с учётом его прошлого, положения и задач. Верни только число.')}>Возраст <small>{String(draft.age ?? 'не задан')}</small></button>
          <button type="button" disabled={busy || locked} onClick={() => address('set_field', { path: 'description' }, 'Улучши только описание главного героя: личность, прошлое, положение в начале, сильные стороны, слабость и внутреннее противоречие. Не меняй известные факты.')}>Описание <small>характер и прошлое</small></button>
          <button type="button" disabled={busy || locked} onClick={() => address('set_field', { path: 'visualAnchorEn' }, 'Улучши только визуальный якорь главного героя. Напиши его на английском: устойчивые черты лица, телосложения, волос, одежды и отличительные детали; без действия, эмоции, позы и фона.')}>Внешность <small>English visual anchor</small></button>
        </div>
      </section>
      <section className="assistant-entities">
        <div className="assistant-entities-head"><div><strong>Цели героя</strong><small>Каждая цель меняется отдельно.</small></div><button type="button" className="ghost compact" disabled={busy || locked} onClick={() => address('add_item', { path: 'goals' }, 'Добавь одну новую личную цель главного героя, которая следует из его прошлого и создаёт решения для игрока. Не повторяй существующие цели.')}>＋ Добавить цель</button></div>
        <div className="assistant-entity-tags">{playerGoals.map(goal => <button type="button" className={selectedItem?.matchValue === goal.matchValue ? 'active' : ''} key={goal.matchValue} onClick={() => setSelectedItem(goal)}>{goal.label}</button>)}</div>
        {selectedItem && <div className="assistant-entity-actions"><button type="button" className="ghost compact" disabled={busy || locked} onClick={() => address('update_item', { path: 'goals', matchField: '', matchValue: selectedItem.matchValue }, `Сделай только цель «${selectedItem.label}» конкретнее и пригодной для решений игрока. Сохрани её исходный смысл.`)}>Уточнить цель</button><button type="button" className="ghost compact danger" disabled={busy || locked} onClick={() => address('remove_item', { path: 'goals', matchField: '', matchValue: selectedItem.matchValue }, `Удали только цель «${selectedItem.label}».`)}>Удалить цель</button></div>}
        {playerGoals.length === 0 && <p className="muted tiny">Целей пока нет — AI может предложить первую, не переписывая профиль героя.</p>}
      </section>
    </>}

    {collection && <section className="assistant-entities"><div className="assistant-entities-head"><strong>{collection.title}</strong>{collection.add && <button type="button" className="ghost compact" disabled={busy || locked} onClick={() => address('add_item', { path: collection.path }, `Добавь одну новую уместную сущность в «${collection.title}». Не изменяй существующие элементы.`)}>＋ {collection.add}</button>}</div>
      <div className="assistant-entity-tags">{collection.items.map(item => <button type="button" className={selectedItem?.matchValue === item.matchValue ? 'active' : ''} key={item.matchValue} onClick={() => setSelectedItem(item)}>{item.label}</button>)}</div>
      {selectedItem && <div className="assistant-entity-actions"><button type="button" className="ghost compact" disabled={busy || locked} onClick={() => address('update_item', { path: collection.path, matchField: selectedItem.matchField, matchValue: selectedItem.matchValue }, `Улучши только «${selectedItem.label}», сохранив его назначение и остальные элементы.`)}>Изменить выбранное</button><button type="button" className="ghost compact danger" disabled={busy || locked} onClick={() => address('remove_item', { path: collection.path, matchField: selectedItem.matchField, matchValue: selectedItem.matchValue }, `Удали только «${selectedItem.label}».`)}>Удалить</button></div>}
    </section>}

    {selected?.key === 'initial_quests' && <section className="assistant-entities"><div className="assistant-entities-head"><strong>Главные и побочные квесты</strong><button type="button" className="ghost compact" disabled={busy || locked} onClick={() => address('add_item', { path: 'quests' }, 'Добавь один новый уместный квест с questType main или side и несколькими стартовыми этапами. Главным делай только центральный сюжет; обычное поручение должно быть побочным. Не изменяй существующие квесты.')}>＋ Квест</button></div>
      <div className="assistant-entity-tags">{quests.map(quest => <button type="button" className={selectedItem?.matchValue === quest.matchValue ? 'active' : ''} key={quest.matchValue} onClick={() => { setSelectedItem(quest); setSelectedStage(null) }}>{quest.label}</button>)}</div>
      {selectedItem && <><div className="assistant-entity-actions"><button type="button" className="ghost compact" disabled={busy || locked} onClick={() => address('update_item', { path: 'quests', matchField: 'title', matchValue: selectedItem.matchValue }, `Улучши только большой квест «${selectedItem.label}», не заменяя его этапы без необходимости.`)}>Изменить квест</button><button type="button" className="ghost compact" disabled={busy || locked} onClick={() => address('add_child_item', { path: 'quests', matchField: 'title', matchValue: selectedItem.matchValue, childPath: 'stages' }, `Добавь один новый стартовый этап в квест «${selectedItem.label}». Не меняй существующие этапы.`)}>＋ Этап</button><button type="button" className="ghost compact danger" disabled={busy || locked} onClick={() => address('remove_item', { path: 'quests', matchField: 'title', matchValue: selectedItem.matchValue }, `Удали только большой квест «${selectedItem.label}» вместе с его этапами.`)}>Удалить квест</button></div>
        <div className="assistant-child-group"><small>Этапы выбранного квеста</small><div className="assistant-entity-tags">{stages.map(stage => <button type="button" className={selectedStage?.matchValue === stage.matchValue ? 'active' : ''} key={stage.matchValue} onClick={() => setSelectedStage(stage)}>{stage.label}</button>)}</div>{selectedStage && <div className="assistant-entity-actions"><button type="button" className="ghost compact" disabled={busy || locked} onClick={() => address('update_child_item', { path: 'quests', matchField: 'title', matchValue: selectedItem.matchValue, childPath: 'stages', childMatchField: 'title', childMatchValue: selectedStage.matchValue }, `Улучши только этап «${selectedStage.label}» в квесте «${selectedItem.label}».`)}>Изменить этап</button><button type="button" className="ghost compact danger" disabled={busy || locked} onClick={() => address('remove_child_item', { path: 'quests', matchField: 'title', matchValue: selectedItem.matchValue, childPath: 'stages', childMatchField: 'title', childMatchValue: selectedStage.matchValue }, `Удали только этап «${selectedStage.label}» из квеста «${selectedItem.label}».`)}>Удалить этап</button></div>}</div>
      </>}
    </section>}

    <label className="setup-assistant-instruction">Своя просьба для этого раздела<textarea rows={5} value={instruction} disabled={busy || locked} onChange={event => setInstruction(event.target.value)} placeholder={`Что изменить в разделе «${selected ? setupComponentTitle(selected.key) : ''}»?`} /></label>
    <button type="button" className="primary setup-assistant-run" disabled={busy || locked || !instruction.trim()} onClick={() => run()}>{busy ? 'Готовлю адресные правки…' : 'Предложить изменения раздела'}</button>
    <button type="button" className="ghost compact assistant-review-all" disabled={busy || editable.length === 0} onClick={() => run('Проверь связи открытого раздела со всем setup и предложи только минимальные необходимые исправления в затронутых разделах.', 'all', 'review')}>Проверить связи между разделами</button>

    {mutation.error && <p className="error" role="alert">{assistantErrorMessage(mutation.error)}</p>}
    {proposal && <section className="setup-assistant-proposal" aria-live="polite"><header><div><p className="eyebrow">Предложение AI</p><strong>{proposal.summary}</strong></div><button type="button" className="ghost compact" onClick={() => setProposal(null)}>Закрыть</button></header>
      {proposal.changes.length === 0 ? <p className="muted">Изменений для применения нет.</p> : <div className="setup-assistant-changes">{proposal.changes.map(change => <article className="setup-assistant-change" key={change.key}><div className="setup-assistant-change-head"><div><strong>{setupComponentTitle(change.key)}</strong><small>{change.operations?.length || change.changedPaths.length} адресных операций</small></div><button type="button" className="ghost compact" disabled={appliedKeys.includes(change.key)} onClick={() => applyChanges([change])}>{appliedKeys.includes(change.key) ? '✓ В черновике' : 'Применить раздел'}</button></div><div className="setup-assistant-operations">{(change.operations ?? []).map((operation, index) => <OperationRow operation={operation} key={`${operation.operation}-${index}`} />)}</div></article>)}</div>}
      {proposal.changes.length > 1 && <button type="button" className="primary setup-assistant-apply-all" disabled={proposal.changes.every(change => appliedKeys.includes(change.key))} onClick={() => applyChanges(proposal.changes)}>Применить всё к черновикам</button>}
      {appliedKeys.length > 0 && <p className="setup-assistant-applied">Изменения находятся только в локальных формах. Проверьте их и нажмите «Сохранить» в соответствующих разделах.</p>}
      <p className="muted assistant-generation-id">generation {proposal.generationId}</p>
    </section>}
  </aside>
}
