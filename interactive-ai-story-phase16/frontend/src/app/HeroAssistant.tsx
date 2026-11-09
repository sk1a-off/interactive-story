import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { applyDirectorExact, assistDirector, type DirectorAssistOperation, type DirectorAssistResult } from '../lib/api'

const labels: Record<string,string> = {
  mood:'Самочувствие и настроение', currentGoal:'Текущая цель', active:'Участие в истории',
  key:'Показатель', value:'Значение', name:'Название', description:'Описание', category:'Категория',
  quantity:'Количество', level:'Уровень', status:'Статус', evidence:'Основание', tags:'Метки',
}
const titles: Record<string,string> = {update_character_state:'Состояние героя',set_stat:'Показатель героя',upsert_journal_entry:'Запись героя'}
const display = (value: unknown): string => {
  if (value === undefined || value === null || value === '') return '—'
  if (typeof value === 'boolean') return value ? 'Да' : 'Нет'
  if (Array.isArray(value)) return value.join(', ')
  if (value === 'active') return 'Активен'
  if (value === 'inactive') return 'Неактивен'
  return String(value)
}

export function HeroAssistant({timelineId,revision,onApplied}:{timelineId:string;revision:number;onApplied:()=>void}) {
  const [instruction,setInstruction] = useState('')
  const [proposal,setProposal] = useState<DirectorAssistResult|null>(null)
  const assist = useMutation({
    mutationFn: (text:string) => assistDirector(timelineId,{instruction:text,section:'hero'}),
    onMutate: () => setProposal(null),
    onSuccess: setProposal,
  })
  const apply = useMutation({
    mutationFn: (operation:DirectorAssistOperation) => {
      if (!proposal || proposal.baseRevision !== revision) throw new Error('История изменилась. Запросите новое предложение.')
      return applyDirectorExact(timelineId,proposal.baseRevision,operation.type,operation.payload,operation.note)
    },
    onSuccess: (_,operation) => {
      setProposal(current => current ? {...current,baseRevision:(current.baseRevision ?? -1)+1,operations:current.operations.filter(item=>item!==operation)} : null)
      onApplied()
    },
  })
  const stale = !!proposal && proposal.operations.length > 0 && proposal.baseRevision !== revision
  return <aside className="director-ai-card" aria-label="ИИ-помощник героя">
    <div className="setup-assistant-head"><div><p className="eyebrow">ИИ-помощник</p><h2>Герой</h2></div><span className="assistant-safe-chip">с подтверждением</span></div>
    <p className="muted tiny">Опишите правку состояния, характеристик, способностей, денег или предметов. Сначала проверьте предложение; история изменится только после применения.</p>
    <div className="setup-assistant-quick">{['Предложи описание новой способности, подходящей герою.','Уточни описание одной способности, сохранив её уровень и ограничения.','Проверь записи героя и предложи точечные исправления без изменения количества вещей и денег.'].map(text=><button key={text} className="ghost compact" disabled={assist.isPending||apply.isPending} onClick={()=>setInstruction(text)}>{text}</button>)}</div>
    <label>Что изменить у героя?<textarea rows={5} value={instruction} onChange={event=>setInstruction(event.target.value)}/></label>
    <button className="primary" disabled={!instruction.trim()||assist.isPending||apply.isPending} onClick={()=>assist.mutate(instruction)}>{assist.isPending?'Готовлю предложение…':'Предложить правки'}</button>
    {assist.error&&<p className="error">{assist.error.message}</p>}
    {apply.error&&<p className="error">{apply.error.message}</p>}
    {stale&&<p role="alert">Состояние истории изменилось или обновляется. Если предложение устарело, запросите новое.</p>}
    {proposal&&<section className="director-ai-proposal"><strong>{proposal.summary}</strong>
      {proposal.operations.map((operation,index)=><article key={`${operation.type}-${index}`}>
        <h3>{titles[operation.type]??operation.type}</h3>
        <dl>{Object.entries(operation.payload).filter(([key,value])=>labels[key]&&JSON.stringify(value)!==JSON.stringify(operation.before?.[key])).map(([key,value])=><div key={key}><dt>{labels[key]}</dt><dd><span>Было: {display(operation.before?.[key])}</span><br/><span>Станет: {display(value)}</span></dd></div>)}</dl>
        {operation.note&&<p>{operation.note}</p>}
        <button className="ghost compact" disabled={stale||apply.isPending||index>0} onClick={()=>apply.mutate(operation)}>{index>0?'Сначала примените предыдущую':'Применить эту правку'}</button>
      </article>)}
      {proposal.operations.length===0&&<p>Нет оставшихся правок для применения.</p>}
      <button className="ghost compact" disabled={apply.isPending} onClick={()=>setProposal(null)}>Закрыть предложение</button>
    </section>}
  </aside>
}
