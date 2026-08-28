import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { editSetup, regenerateSetup, setSetupLock, type SetupComponent } from '../lib/api'
import { clearSetupDraftPayload, readSetupDraftPayload, setupComponentDescription, setupComponentTitle, writeSetupDraftPayload, type SetupPayload } from './setupEditorModel'

type Payload = SetupPayload
type Row = Record<string, unknown>

function asText(value: unknown): string {
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return ''
}

function asNumberText(value: unknown): string {
  return typeof value === 'number' && Number.isFinite(value) ? String(value) : asText(value)
}

function asTextList(value: unknown): string[] {
  if (Array.isArray(value)) return value.map(asText).filter(Boolean)
  const one = asText(value).trim()
  return one ? [one] : []
}

function asRows(value: unknown, fallbackKey: string): Row[] {
  if (!Array.isArray(value)) return []
  return value.map((item) => {
    if (item && typeof item === 'object' && !Array.isArray(item)) return { ...(item as Row) }
    const text = asText(item)
    return text ? { [fallbackKey]: text } : {}
  })
}

function asStringMap(value: unknown): Record<string, string> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
  return Object.fromEntries(Object.entries(value as Record<string, unknown>).map(([key, val]) => [key, asText(val)]))
}

function replace(payload: Payload, key: string, value: unknown): Payload {
  return { ...payload, [key]: value }
}

function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return <label className="setup-field"><span>{label}</span>{hint && <small>{hint}</small>}{children}</label>
}

function TextListEditor({ label, hint, values, placeholder, onChange, disabled }: { label: string; hint?: string; values: string[]; placeholder?: string; onChange: (values: string[]) => void; disabled?: boolean }) {
  const rows = values.length ? values : ['']
  const setAt = (index: number, value: string) => {
    const next = [...rows]
    next[index] = value
    onChange(next)
  }
  const remove = (index: number) => onChange(rows.filter((_, i) => i !== index))
  return <section className="setup-subsection">
    <div className="setup-subsection-head"><div><strong>{label}</strong>{hint && <small>{hint}</small>}</div><button type="button" className="ghost compact" disabled={disabled} onClick={() => onChange([...rows, ''])}>+ Добавить</button></div>
    <div className="setup-list">
      {rows.map((value, index) => <div className="setup-list-row" key={index}>
        <input value={value} disabled={disabled} placeholder={placeholder} onChange={(e) => setAt(index, e.target.value)} />
        <button type="button" className="icon-button" disabled={disabled || rows.length === 1} onClick={() => remove(index)} aria-label="Удалить">×</button>
      </div>)}
    </div>
  </section>
}

function StringMapEditor({ label, hint, value, keyPlaceholder, valuePlaceholder, onChange, disabled }: { label: string; hint?: string; value: Record<string, string>; keyPlaceholder: string; valuePlaceholder: string; onChange: (value: Record<string, string>) => void; disabled?: boolean }) {
  const valueSignature = JSON.stringify(Object.entries(value))
  const [rows, setRows] = useState(() => Object.entries(value).map(([key, val]) => ({ key, value: val })))
  useEffect(() => {
    const entries = JSON.parse(valueSignature) as Array<[string, string]>
    setRows(entries.map(([key, val]) => ({ key, value: val })))
  }, [valueSignature])
  const visible = rows.length ? rows : [{ key: '', value: '' }]
  const commit = (next: Array<{ key: string; value: string }>) => {
    setRows(next)
    onChange(Object.fromEntries(next.map((row) => [row.key.trim(), row.value]).filter(([key]) => key)))
  }
  return <section className="setup-subsection">
    <div className="setup-subsection-head"><div><strong>{label}</strong>{hint && <small>{hint}</small>}</div><button type="button" className="ghost compact" disabled={disabled} onClick={() => setRows([...visible, { key: '', value: '' }])}>+ Добавить</button></div>
    <div className="setup-map-list">
      {visible.map((row, index) => <div className="setup-map-row" key={index}>
        <input value={row.key} disabled={disabled} placeholder={keyPlaceholder} onChange={(e) => { const next = [...visible]; next[index] = { ...row, key: e.target.value }; commit(next) }} />
        <textarea rows={2} value={row.value} disabled={disabled} placeholder={valuePlaceholder} onChange={(e) => { const next = [...visible]; next[index] = { ...row, value: e.target.value }; commit(next) }} />
        <button type="button" className="icon-button" disabled={disabled || visible.length === 1} onClick={() => commit(visible.filter((_, i) => i !== index))} aria-label="Удалить">×</button>
      </div>)}
    </div>
  </section>
}

function StoryBibleFields({ payload, setPayload, disabled }: FormProps) {
  return <div className="setup-form-grid">
    <Field label="Основная идея" hint="Конкретная завязка и центральная ситуация истории."><textarea rows={6} disabled={disabled} value={asText(payload.premise)} onChange={(e) => setPayload(replace(payload, 'premise', e.target.value))} /></Field>
    <TextListEditor label="Тон" hint="2–5 характеристик атмосферы." values={asTextList(payload.tone)} placeholder="например: мрачный, камерный" disabled={disabled} onChange={(values) => setPayload(replace(payload, 'tone', values))} />
    <TextListEditor label="Темы" values={asTextList(payload.themes)} placeholder="доверие, взросление, власть…" disabled={disabled} onChange={(values) => setPayload(replace(payload, 'themes', values))} />
    <TextListEditor label="Правила повествования" hint="POV, непрерывность, ограничения и важные авторские правила." values={asTextList(payload.narrativeRules)} placeholder="Сохранять повествование от первого лица" disabled={disabled} onChange={(values) => setPayload(replace(payload, 'narrativeRules', values))} />
  </div>
}

function PlayerFields({ payload, setPayload, disabled }: FormProps) {
  return <div className="setup-form-grid two-columns">
    <Field label="Имя"><input disabled={disabled} value={asText(payload.name)} onChange={(e) => setPayload(replace(payload, 'name', e.target.value))} /></Field>
    <Field label="Возраст" hint="Главный герой должен быть совершеннолетним."><input type="number" min={18} disabled={disabled} value={asNumberText(payload.age)} onChange={(e) => setPayload(replace(payload, 'age', e.target.value === '' ? '' : Number(e.target.value)))} /></Field>
    <div className="span-two"><Field label="Описание" hint="Личность, прошлое и положение героя в начале истории."><textarea rows={6} disabled={disabled} value={asText(payload.description)} onChange={(e) => setPayload(replace(payload, 'description', e.target.value))} /></Field></div>
    <div className="span-two"><TextListEditor label="Цели" values={asTextList(payload.goals)} placeholder="Чего герой хочет добиться" disabled={disabled} onChange={(values) => setPayload(replace(payload, 'goals', values))} /></div>
    <div className="span-two"><Field label="Внешность для генерации изображений" hint="На английском. Стабильные черты внешности и одежда, без описания конкретного действия."><textarea className="english-field" rows={5} disabled={disabled} value={asText(payload.visualAnchorEn)} onChange={(e) => setPayload(replace(payload, 'visualAnchorEn', e.target.value))} /></Field></div>
  </div>
}

function WorldFields({ payload, setPayload, disabled }: FormProps) {
  const locations = asRows(payload.locations, 'name')
  const shownLocations = locations.length ? locations : [{ name: '', description: '', visualAnchorEn: '' }]
  const setLocation = (index: number, field: string, value: string) => {
    const next = [...shownLocations]
    next[index] = { ...next[index], [field]: value }
    setPayload(replace(payload, 'locations', next))
  }
  return <div className="setup-form-grid two-columns">
    <Field label="Название мира"><input disabled={disabled} value={asText(payload.name)} onChange={(e) => setPayload(replace(payload, 'name', e.target.value))} /></Field>
    <div className="span-two"><Field label="Состояние мира" hint="Что важно знать о мире именно на момент начала этой истории."><textarea rows={6} disabled={disabled} value={asText(payload.summary)} onChange={(e) => setPayload(replace(payload, 'summary', e.target.value))} /></Field></div>
    <section className="setup-subsection span-two">
      <div className="setup-subsection-head"><div><strong>Важные локации</strong><small>Места, которые будут регулярно появляться в истории.</small></div><button type="button" className="ghost compact" disabled={disabled} onClick={() => setPayload(replace(payload, 'locations', [...shownLocations, { name: '', description: '', visualAnchorEn: '' }]))}>+ Локация</button></div>
      <div className="setup-record-list">
        {shownLocations.map((location, index) => <article className="setup-record-card" key={index}>
          <div className="setup-record-head"><strong>Локация {index + 1}</strong><button type="button" className="icon-button" disabled={disabled || shownLocations.length === 1} onClick={() => setPayload(replace(payload, 'locations', shownLocations.filter((_, i) => i !== index)))}>×</button></div>
          <Field label="Название"><input disabled={disabled} value={asText(location.name)} onChange={(e) => setLocation(index, 'name', e.target.value)} /></Field>
          <Field label="Описание"><textarea rows={3} disabled={disabled} value={asText(location.description)} onChange={(e) => setLocation(index, 'description', e.target.value)} /></Field>
          <Field label="Visual anchor" hint="Необязательно, на английском."><textarea className="english-field" rows={2} disabled={disabled} value={asText(location.visualAnchorEn)} onChange={(e) => setLocation(index, 'visualAnchorEn', e.target.value)} /></Field>
        </article>)}
      </div>
    </section>
    <div className="span-two"><StringMapEditor label="Общие visual anchors локаций" hint="Для совместимости с существующим visualAnchorsEn." value={asStringMap(payload.visualAnchorsEn)} keyPlaceholder="Название локации" valuePlaceholder="English visual description" disabled={disabled} onChange={(value) => setPayload(replace(payload, 'visualAnchorsEn', value))} /></div>
  </div>
}

function WorldRulesFields({ payload, setPayload, disabled }: FormProps) {
  const systems = asRows(payload.systems, 'name')
  const shownSystems = systems.length ? systems : [{ id: 'world_foundations', name: '', kind: 'world', description: '', resources: [] }]
  const rules = asRows(payload.rules, 'title')
  const shownRules = rules.length ? rules : [{ id: 'RULE-001', systemId: asText(shownSystems[0]?.id), title: '', category: 'law', severity: 'hard', statement: '', preconditions: [], costs: [], forbiddenResults: [], exceptions: [], tags: [], visibility: 'canon_only', status: 'established' }]
  const glossary = asRows(payload.glossary, 'term')
  const setSystem = (index: number, patch: Row) => { const next = [...shownSystems]; next[index] = { ...next[index], ...patch }; setPayload(replace(payload, 'systems', next)) }
  const setRule = (index: number, patch: Row) => { const next = [...shownRules]; next[index] = { ...next[index], ...patch }; setPayload(replace(payload, 'rules', next)) }
  return <div className="world-rules-editor">
    <section className="setup-subsection">
      <div className="setup-subsection-head"><div><strong>Системы мира</strong><small>Отдельные механизмы: магия, нейросеть, технологии, божественные силы.</small></div><button type="button" className="ghost compact" disabled={disabled} onClick={() => setPayload(replace(payload, 'systems', [...shownSystems, { id: `system_${shownSystems.length + 1}`, name: '', kind: 'other', description: '', resources: [] }]))}>＋ Система</button></div>
      <div className="setup-record-list">{shownSystems.map((system, index) => { const resources = asRows(system.resources, 'name'); return <article className="setup-record-card world-system-card" key={index}>
        <div className="setup-record-head"><strong>{asText(system.name) || `Система ${index + 1}`}</strong><button type="button" className="icon-button" disabled={disabled || shownSystems.length === 1} onClick={() => setPayload(replace(payload, 'systems', shownSystems.filter((_, i) => i !== index)))}>×</button></div>
        <div className="setup-inline-grid"><Field label="Стабильный ID"><input disabled={disabled} value={asText(system.id)} onChange={e => setSystem(index, { id: e.target.value })} /></Field><Field label="Тип"><select disabled={disabled} value={asText(system.kind) || 'other'} onChange={e => setSystem(index, { kind: e.target.value })}><option value="world">Законы мира</option><option value="magic">Магия</option><option value="neural">Нейросеть</option><option value="technology">Технология</option><option value="divine">Божественное</option><option value="mental">Ментальное</option><option value="social">Социальное</option><option value="other">Другое</option></select></Field></div>
        <Field label="Название"><input disabled={disabled} value={asText(system.name)} onChange={e => setSystem(index, { name: e.target.value })} /></Field>
        <Field label="Принцип работы"><textarea rows={3} disabled={disabled} value={asText(system.description)} onChange={e => setSystem(index, { description: e.target.value })} /></Field>
        <div className="setup-subsection-head nested"><strong>Измеримые ресурсы</strong><button type="button" className="ghost compact" disabled={disabled} onClick={() => setSystem(index, { resources: [...resources, { id: `resource_${resources.length + 1}`, name: '', unit: '', ownerScope: 'hero', initialValue: 0, minValue: 0 }] })}>＋ Ресурс</button></div>
        <div className="world-resource-list">{resources.map((resource, resourceIndex) => <div className="world-resource-row" key={resourceIndex}><input disabled={disabled} placeholder="id" value={asText(resource.id)} onChange={e => { const next = [...resources]; next[resourceIndex] = { ...resource, id: e.target.value }; setSystem(index, { resources: next }) }} /><input disabled={disabled} placeholder="Название" value={asText(resource.name)} onChange={e => { const next = [...resources]; next[resourceIndex] = { ...resource, name: e.target.value }; setSystem(index, { resources: next }) }} /><input disabled={disabled} placeholder="Единица" value={asText(resource.unit)} onChange={e => { const next = [...resources]; next[resourceIndex] = { ...resource, unit: e.target.value }; setSystem(index, { resources: next }) }} /><input type="number" disabled={disabled} aria-label="Начальное значение" value={asNumberText(resource.initialValue)} onChange={e => { const next = [...resources]; next[resourceIndex] = { ...resource, initialValue: numberOrEmpty(e.target.value) }; setSystem(index, { resources: next }) }} /><button type="button" className="icon-button" disabled={disabled} onClick={() => setSystem(index, { resources: resources.filter((_, i) => i !== resourceIndex) })}>×</button></div>)}</div>
      </article> })}</div>
    </section>
    <section className="setup-subsection">
      <div className="setup-subsection-head"><div><strong>Адресные законы</strong><small>Жёсткие правила проверяются перед публикацией каждого фрагмента.</small></div><button type="button" className="ghost compact" disabled={disabled} onClick={() => setPayload(replace(payload, 'rules', [...shownRules, { id: `RULE-${String(shownRules.length + 1).padStart(3, '0')}`, systemId: asText(shownSystems[0]?.id), title: '', category: 'law', severity: 'hard', statement: '', preconditions: [], costs: [], forbiddenResults: [], exceptions: [], tags: [], visibility: 'canon_only', status: 'established' }]))}>＋ Закон</button></div>
      <div className="setup-quest-list">{shownRules.map((rule, index) => <article className="setup-quest-card world-rule-card" key={index}>
        <div className="setup-record-head"><div><span className={`rule-severity ${asText(rule.severity)}`}>{asText(rule.severity) || 'hard'}</span><strong>{asText(rule.title) || `Закон ${index + 1}`}</strong></div><button type="button" className="icon-button" disabled={disabled || shownRules.length === 1} onClick={() => setPayload(replace(payload, 'rules', shownRules.filter((_, i) => i !== index)))}>×</button></div>
        <div className="setup-inline-grid"><Field label="ID правила"><input disabled={disabled} value={asText(rule.id)} onChange={e => setRule(index, { id: e.target.value })} /></Field><Field label="Система"><select disabled={disabled} value={asText(rule.systemId)} onChange={e => setRule(index, { systemId: e.target.value })}>{shownSystems.map((system, i) => <option key={i} value={asText(system.id)}>{asText(system.name) || asText(system.id)}</option>)}</select></Field><Field label="Сила"><select disabled={disabled} value={asText(rule.severity) || 'hard'} onChange={e => setRule(index, { severity: e.target.value })}><option value="hard">Жёсткий закон</option><option value="soft">Мягкое правило</option><option value="mystery">Скрытая истина</option><option value="belief">Убеждение</option></select></Field><Field label="Категория"><select disabled={disabled} value={asText(rule.category) || 'law'} onChange={e => setRule(index, { category: e.target.value })}>{['axiom','law','mechanism','limit','cost','progression','exception','social','terminology'].map(value => <option value={value} key={value}>{value}</option>)}</select></Field></div>
        <Field label="Название"><input disabled={disabled} value={asText(rule.title)} onChange={e => setRule(index, { title: e.target.value })} /></Field><Field label="Точное утверждение"><textarea rows={3} disabled={disabled} value={asText(rule.statement)} onChange={e => setRule(index, { statement: e.target.value })} /></Field>
        <TextListEditor label="Условия применения" values={asTextList(rule.preconditions)} disabled={disabled} onChange={values => setRule(index, { preconditions: values })} /><TextListEditor label="Цена" values={asTextList(rule.costs)} disabled={disabled} onChange={values => setRule(index, { costs: values })} /><TextListEditor label="Запрещённые результаты" values={asTextList(rule.forbiddenResults)} disabled={disabled} onChange={values => setRule(index, { forbiddenResults: values })} /><TextListEditor label="Исключения" values={asTextList(rule.exceptions)} disabled={disabled} onChange={values => setRule(index, { exceptions: values })} />
        <div className="setup-inline-grid"><Field label="Видимость"><select disabled={disabled} value={asText(rule.visibility) || 'canon_only'} onChange={e => setRule(index, { visibility: e.target.value })}><option value="canon_only">Только канон</option><option value="known_to_hero">Известно герою</option><option value="public">Известно всем</option><option value="hidden">Скрыто</option></select></Field><Field label="Статус"><select disabled={disabled} value={asText(rule.status) || 'established'} onChange={e => setRule(index, { status: e.target.value })}><option value="established">Канон</option><option value="pending">Предложение</option><option value="superseded">Заменено</option><option value="archived">Архив</option></select></Field></div>
      </article>)}</div>
    </section>
    <section className="setup-subsection"><div className="setup-subsection-head"><div><strong>Словарь терминов</strong><small>Одно значение для важных слов мира.</small></div><button type="button" className="ghost compact" disabled={disabled} onClick={() => setPayload(replace(payload, 'glossary', [...glossary, { term: '', definition: '' }]))}>＋ Термин</button></div><div className="setup-map-list">{glossary.map((entry, index) => <div className="setup-map-row" key={index}><input disabled={disabled} placeholder="Термин" value={asText(entry.term)} onChange={e => { const next = [...glossary]; next[index] = { ...entry, term: e.target.value }; setPayload(replace(payload, 'glossary', next)) }} /><textarea rows={2} disabled={disabled} placeholder="Однозначное определение" value={asText(entry.definition)} onChange={e => { const next = [...glossary]; next[index] = { ...entry, definition: e.target.value }; setPayload(replace(payload, 'glossary', next)) }} /><button type="button" className="icon-button" disabled={disabled} onClick={() => setPayload(replace(payload, 'glossary', glossary.filter((_, i) => i !== index)))}>×</button></div>)}</div></section>
  </div>
}

function numberOrEmpty(value: string): number | string { return value === '' ? '' : Number(value) }

function InitialCastFields({ payload, setPayload, disabled }: FormProps) {
  const characters = asRows(payload.characters, 'name')
  const shown = characters.length ? characters : [{ name: '', role: '', personality: '', relationship: '', visualAnchorEn: '' }]
  const setCharacter = (index: number, field: string, value: string | number) => {
    const next = [...shown]
    next[index] = { ...next[index], [field]: value }
    setPayload(replace(payload, 'characters', next))
  }
  return <section className="setup-subsection">
    <div className="setup-subsection-head"><div><strong>Стартовые персонажи</strong><small>Добавляйте только тех, кто действительно важен в начале истории.</small></div><button type="button" className="ghost compact" disabled={disabled} onClick={() => setPayload(replace(payload, 'characters', [...shown, { name: '', role: '', personality: '', relationship: '', visualAnchorEn: '' }]))}>+ Персонаж</button></div>
    <div className="setup-record-list cast-list">
      {shown.map((character, index) => <article className="setup-record-card" key={index}>
        <div className="setup-record-head"><strong>{asText(character.name) || `Персонаж ${index + 1}`}</strong><button type="button" className="icon-button" disabled={disabled || shown.length === 1} onClick={() => setPayload(replace(payload, 'characters', shown.filter((_, i) => i !== index)))}>×</button></div>
        <div className="setup-inline-grid">
          <Field label="Имя"><input disabled={disabled} value={asText(character.name)} onChange={(e) => setCharacter(index, 'name', e.target.value)} /></Field>
          <Field label="Возраст"><input type="number" disabled={disabled} value={asNumberText(character.age)} onChange={(e) => setCharacter(index, 'age', e.target.value === '' ? '' : Number(e.target.value))} /></Field>
        </div>
        <Field label="Роль"><input disabled={disabled} value={asText(character.role)} onChange={(e) => setCharacter(index, 'role', e.target.value)} placeholder="друг, соперник, наставник…" /></Field>
        <Field label="Характер"><textarea rows={3} disabled={disabled} value={asText(character.personality)} onChange={(e) => setCharacter(index, 'personality', e.target.value)} /></Field>
        <Field label="Отношение к герою"><textarea rows={3} disabled={disabled} value={asText(character.relationship)} onChange={(e) => setCharacter(index, 'relationship', e.target.value)} /></Field>
        <Field label="Внешность для изображений" hint="На английском."><textarea className="english-field" rows={3} disabled={disabled} value={asText(character.visualAnchorEn)} onChange={(e) => setCharacter(index, 'visualAnchorEn', e.target.value)} /></Field>
      </article>)}
    </div>
  </section>
}

function VisualBibleFields({ payload, setPayload, disabled }: FormProps) {
  return <div className="setup-form-grid two-columns">
    <Field label="Стиль"><textarea rows={4} disabled={disabled} value={asText(payload.style)} onChange={(e) => setPayload(replace(payload, 'style', e.target.value))} /></Field>
    <Field label="Палитра и освещение"><textarea rows={4} disabled={disabled} value={asText(payload.palette)} onChange={(e) => setPayload(replace(payload, 'palette', e.target.value))} /></Field>
    <div className="span-two"><Field label="Кинематография" hint="Камера, композиция, глубина кадра и предпочитаемый визуальный язык."><textarea rows={4} disabled={disabled} value={asText(payload.cinematography)} onChange={(e) => setPayload(replace(payload, 'cinematography', e.target.value))} /></Field></div>
    <div className="span-two"><TextListEditor label="Правила визуальной непрерывности" values={asTextList(payload.continuityRules)} placeholder="Не менять базовую одежду внутри одной сцены" disabled={disabled} onChange={(values) => setPayload(replace(payload, 'continuityRules', values))} /></div>
    <div className="span-two"><StringMapEditor label="Заметки о внешности персонажей" value={asStringMap(payload.characterNotes)} keyPlaceholder="Имя персонажа" valuePlaceholder="English reusable appearance anchor" disabled={disabled} onChange={(value) => setPayload(replace(payload, 'characterNotes', value))} /></div>
    <div className="span-two"><Field label="Глобальный negative prompt" hint="На английском. Что не должно появляться в изображениях."><textarea className="english-field" rows={5} disabled={disabled} value={asText(payload.negativePromptEn)} onChange={(e) => setPayload(replace(payload, 'negativePromptEn', e.target.value))} /></Field></div>
  </div>
}

function InitialQuestsFields({ payload, setPayload, disabled }: FormProps) {
  const quests = asRows(payload.quests, 'title')
  const shown = quests.length ? quests : [{ questType: 'main', title: '', description: '', successCriteria: '', stages: [] }]
  const commitQuest = (index: number, patch: Row) => {
    const next = [...shown]
    next[index] = { ...next[index], ...patch }
    setPayload(replace(payload, 'quests', next))
  }
  const addQuest = () => setPayload(replace(payload, 'quests', [...shown, { questType: 'side', title: '', description: '', successCriteria: '', stages: [{ kind: 'task', title: '', description: '', successCriteria: '' }] }]))
  return <section className="setup-subsection quest-setup-editor">
    <div className="setup-subsection-head"><div><strong>Главные и побочные квесты</strong><small>У каждой линии могут быть собственные задачи, события и рубежи.</small></div><button type="button" className="ghost compact" disabled={disabled} onClick={addQuest}>+ Квест</button></div>
    <div className="setup-quest-list">{shown.map((quest, questIndex) => {
      const stages = asRows(quest.stages, 'title')
      const shownStages = stages.length ? stages : [{ kind: 'task', title: '', description: '', successCriteria: '' }]
      const setStage = (stageIndex: number, patch: Row) => {
        const next = [...shownStages]
        next[stageIndex] = { ...next[stageIndex], ...patch }
        commitQuest(questIndex, { stages: next })
      }
      return <article className="setup-quest-card" key={questIndex}>
        <div className="setup-record-head"><div><span className="objective-kind">{asText(quest.questType)==='side'?'Побочный':'Главный'} квест {questIndex + 1}</span><strong>{asText(quest.title) || 'Новый квест'}</strong></div><button type="button" className="icon-button" disabled={disabled || shown.length === 1} onClick={() => setPayload(replace(payload, 'quests', shown.filter((_, index) => index !== questIndex)))} aria-label="Удалить квест">×</button></div>
        <Field label="Линия"><select disabled={disabled} value={asText(quest.questType) || 'main'} onChange={(event) => commitQuest(questIndex, { questType: event.target.value })}><option value="main">Главный квест</option><option value="side">Побочный квест</option></select></Field>
        <Field label="Название"><input disabled={disabled} value={asText(quest.title)} onChange={(event) => commitQuest(questIndex, { title: event.target.value })} /></Field>
        <Field label="Описание"><textarea rows={3} disabled={disabled} value={asText(quest.description)} onChange={(event) => commitQuest(questIndex, { description: event.target.value })} /></Field>
        <Field label="Условие завершения"><textarea rows={2} disabled={disabled} value={asText(quest.successCriteria)} onChange={(event) => commitQuest(questIndex, { successCriteria: event.target.value })} /></Field>
        <section className="setup-subsection quest-stage-editor">
          <div className="setup-subsection-head"><div><strong>Стартовые этапы</strong><small>Это известные в начале шаги. Новые этапы смогут появляться по ходу сюжета.</small></div><button type="button" className="ghost compact" disabled={disabled} onClick={() => commitQuest(questIndex, { stages: [...shownStages, { kind: 'task', title: '', description: '', successCriteria: '' }] })}>+ Этап</button></div>
          <div className="setup-stage-list">{shownStages.map((stage, stageIndex) => <article className="setup-stage-card" key={stageIndex}>
            <div className="setup-record-head"><strong>{asText(stage.title) || `Этап ${stageIndex + 1}`}</strong><button type="button" className="icon-button" disabled={disabled || shownStages.length === 1} onClick={() => commitQuest(questIndex, { stages: shownStages.filter((_, index) => index !== stageIndex) })} aria-label="Удалить этап">×</button></div>
            <Field label="Тип"><select disabled={disabled} value={asText(stage.kind) || 'task'} onChange={(event) => setStage(stageIndex, { kind: event.target.value })}><option value="task">Задача</option><option value="event">Событие</option><option value="milestone">Рубеж</option></select></Field>
            <Field label="Название"><input disabled={disabled} value={asText(stage.title)} onChange={(event) => setStage(stageIndex, { title: event.target.value })} /></Field>
            <Field label="Описание"><textarea rows={2} disabled={disabled} value={asText(stage.description)} onChange={(event) => setStage(stageIndex, { description: event.target.value })} /></Field>
            <Field label="Условие выполнения"><textarea rows={2} disabled={disabled} value={asText(stage.successCriteria)} onChange={(event) => setStage(stageIndex, { successCriteria: event.target.value })} /></Field>
          </article>)}</div>
        </section>
      </article>
    })}</div>
  </section>
}

function OpeningFields({ payload, setPayload, disabled }: FormProps) {
  const choices = [...asTextList(payload.choices)]
  while (choices.length < 4) choices.push('')
  const shown = choices.slice(0, 4)
  const setChoice = (index: number, value: string) => {
    const next = [...shown]
    next[index] = value
    setPayload(replace(payload, 'choices', next))
  }
  return <div className="setup-form-grid two-columns">
    <Field label="Название главы"><input disabled={disabled} value={asText(payload.chapterTitle)} onChange={(e) => setPayload(replace(payload, 'chapterTitle', e.target.value))} /></Field>
    <Field label="Цель главы"><input disabled={disabled} value={asText(payload.chapterGoal)} onChange={(e) => setPayload(replace(payload, 'chapterGoal', e.target.value))} /></Field>
    <div className="span-two"><Field label="Цель первой сцены"><input disabled={disabled} value={asText(payload.sceneGoal)} onChange={(e) => setPayload(replace(payload, 'sceneGoal', e.target.value))} /></Field></div>
    <div className="span-two"><Field label="Текст вступления" hint="Это канонический первый beat истории. Можно свободно редактировать до запуска."><textarea className="opening-editor" rows={18} disabled={disabled} value={asText(payload.text)} onChange={(e) => setPayload(replace(payload, 'text', e.target.value))} /></Field></div>
    <section className="setup-subsection span-two"><div className="setup-subsection-head"><div><strong>Стартовые действия</strong><small>Четыре разных намерения игрока, а не гарантированные результаты.</small></div></div>
      <div className="opening-choices">{shown.map((choice, index) => <Field key={index} label={`Вариант ${index + 1}`}><textarea rows={2} disabled={disabled} value={choice} onChange={(e) => setChoice(index, e.target.value)} /></Field>)}</div>
    </section>
  </div>
}

type FormProps = { payload: Payload; setPayload: (payload: Payload) => void; disabled?: boolean }

function StructuredFields({ componentKey, payload, setPayload, disabled }: FormProps & { componentKey: string }) {
  switch (componentKey) {
    case 'story_bible': return <StoryBibleFields payload={payload} setPayload={setPayload} disabled={disabled} />
    case 'player': return <PlayerFields payload={payload} setPayload={setPayload} disabled={disabled} />
    case 'world': return <WorldFields payload={payload} setPayload={setPayload} disabled={disabled} />
    case 'world_rules': return <WorldRulesFields payload={payload} setPayload={setPayload} disabled={disabled} />
    case 'initial_cast': return <InitialCastFields payload={payload} setPayload={setPayload} disabled={disabled} />
    case 'visual_bible': return <VisualBibleFields payload={payload} setPayload={setPayload} disabled={disabled} />
    case 'initial_quests': return <InitialQuestsFields payload={payload} setPayload={setPayload} disabled={disabled} />
    case 'opening_situation': return <OpeningFields payload={payload} setPayload={setPayload} disabled={disabled} />
    default: return null
  }
}

export function SetupComponentEditor({ storyId, component, draftEpoch = 0 }: { storyId: string; component: SetupComponent; draftEpoch?: number }) {
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<Payload>(() => readSetupDraftPayload(storyId, component))
  const [rawDraft, setRawDraft] = useState(() => JSON.stringify(readSetupDraftPayload(storyId, component), null, 2))
  const [rawError, setRawError] = useState('')

  useEffect(() => {
    const restored = readSetupDraftPayload(storyId, component)
    setDraft(restored)
    setRawDraft(JSON.stringify(restored, null, 2))
    setRawError('')
  }, [storyId, component, draftEpoch])

  const canonicalDraft = useMemo(() => JSON.stringify(draft), [draft])
  const canonicalCurrent = useMemo(() => JSON.stringify(component.payload), [component.payload])
  const dirty = canonicalDraft !== canonicalCurrent
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['setup', storyId] })

  const save = useMutation({
    mutationFn: () => editSetup(storyId, component.key, draft, component.locked),
    onSuccess: () => { clearSetupDraftPayload(storyId, component); return refresh() },
  })
  const regenerate = useMutation({ mutationFn: () => regenerateSetup(storyId, component.key), onSuccess: refresh })
  const lock = useMutation({ mutationFn: () => setSetupLock(storyId, component.key, !component.locked), onSuccess: refresh })

  const updateDraft = (next: Payload) => {
    setDraft(next)
    setRawDraft(JSON.stringify(next, null, 2))
    setRawError('')
    writeSetupDraftPayload(storyId, component, next)
  }
  const discardDraft = () => {
    clearSetupDraftPayload(storyId, component)
    setDraft({ ...component.payload })
    setRawDraft(JSON.stringify(component.payload, null, 2))
    setRawError('')
  }

  const applyRaw = () => {
    try {
      const parsed = JSON.parse(rawDraft) as unknown
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('Корень должен быть JSON-объектом')
      updateDraft(parsed as Payload)
    } catch (error) {
      setRawError(error instanceof Error ? error.message : 'Некорректный JSON')
    }
  }

  const busy = save.isPending || regenerate.isPending || lock.isPending
  return <article className="setup-editor-card">
    <header className="setup-editor-head">
      <div><p className="eyebrow">{component.status}</p><h1>{setupComponentTitle(component.key)}</h1><p className="muted">{setupComponentDescription(component.key)}</p><div className="setup-meta"><span>rev {component.revision}</span><span>{component.source === 'ai' ? 'AI' : component.source === 'manual' ? 'вручную' : component.source}</span>{component.locked && <span className="locked-chip">🔒 зафиксировано</span>}{dirty && <span className="dirty-chip">есть изменения</span>}</div></div>
      <button type="button" className="ghost" disabled={busy} onClick={() => lock.mutate()}>{component.locked ? 'Разблокировать' : 'Зафиксировать'}</button>
    </header>

    {component.locked && <p className="setup-notice">Компонент зафиксирован для AI-регенерации. Ручные правки разрешены и сохранят эту блокировку.</p>}
    <StructuredFields componentKey={component.key} payload={draft} setPayload={updateDraft} disabled={busy} />

    <details className="advanced-json">
      <summary>Дополнительно: сырой JSON</summary>
      <p className="muted tiny">Нужен только для отладки или редких полей, которых пока нет в форме. Обычные изменения выше автоматически превращаются в этот JSON.</p>
      <textarea className="json-editor" rows={12} disabled={busy} value={rawDraft} onChange={(e) => setRawDraft(e.target.value)} />
      {rawError && <p className="error">{rawError}</p>}
      <div className="actions"><button type="button" disabled={busy} onClick={applyRaw}>Применить JSON к форме</button><button type="button" disabled={busy} onClick={() => { setRawDraft(JSON.stringify(draft, null, 2)); setRawError('') }}>Отменить правки JSON</button></div>
    </details>

    {(save.error || regenerate.error || lock.error) && <p className="error">{save.error?.message || regenerate.error?.message || lock.error?.message}</p>}
    <footer className="setup-savebar">
      <div><strong>{dirty ? 'Изменения ещё не сохранены' : 'Все изменения сохранены'}</strong><small>На сервер отправляется структурированный JSON, форма остаётся удобной для редактирования.</small></div>
      <div className="actions">{dirty && <button type="button" disabled={busy} onClick={discardDraft}>Сбросить правки</button>}<button type="button" disabled={component.locked || busy || dirty} title={dirty ? 'Сначала сохраните или сбросьте ручные правки' : undefined} onClick={() => regenerate.mutate()}>{regenerate.isPending ? 'Генерирую…' : 'Перегенерировать AI'}</button><button type="button" className="primary" disabled={busy || !dirty} onClick={() => save.mutate()}>{save.isPending ? 'Сохраняю…' : 'Сохранить'}</button></div>
    </footer>
  </article>
}
