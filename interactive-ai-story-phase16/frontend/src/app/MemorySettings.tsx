import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getMemoryStatus, reindexMemory } from '../lib/api'
import '../styles/memory-settings.css'

export type MemoryOptions={enabled:boolean;topK:number;tokenBudget:number}
export function MemorySettings({value,onChange,saved,revision}:{value:MemoryOptions;onChange:(value:MemoryOptions)=>void;saved:MemoryOptions;revision:string}){
 const qc=useQueryClient()
 const status=useQuery({queryKey:['memory-status',revision],queryFn:getMemoryStatus,refetchInterval:5000,retry:false})
 const rebuild=useMutation({mutationFn:reindexMemory,onSuccess:()=>qc.invalidateQueries({queryKey:['memory-status']})})
 const dirty=value.enabled!==saved.enabled||value.topK!==saved.topK||value.tokenBudget!==saved.tokenBudget
 const data=status.data
 const building=data?.state==='building'
 const label=status.isPending?'Проверяю состояние…':status.isError?'Статус недоступен':!data?.enabled?'Выключена':building?'Обновляю память':data.state==='serving'?'Готова':data.state==='degraded'?'Память требует обновления':data.state==='updating'?'Запоминаю новые события':data.state==='failed'?'Не удалось обновить память':'Подготовка памяти'
 return <fieldset className="image-output-settings memory-settings"><legend>Память истории</legend>
  <label className="memory-toggle"><input type="checkbox" checked={value.enabled} onChange={e=>onChange({...value,enabled:e.target.checked})}/> Использовать память прошлых событий</label>
  <small className="muted">Помогает вспоминать старые события с учётом ветки истории. Индексирование работает в фоне. При недоступности памяти продолжение создаётся без неё.</small>
  <label>Максимум воспоминаний<input type="number" min={1} max={100} step={1} value={value.topK} onChange={e=>onChange({...value,topK:e.currentTarget.valueAsNumber||0})}/></label>
  <label>Объём памяти в запросе, токенов<input type="number" min={256} max={8192} step={256} value={value.tokenBudget} onChange={e=>onChange({...value,tokenBudget:e.currentTarget.valueAsNumber||0})}/><small className="muted">Больше объём — длиннее запрос. По умолчанию 1024. Текущее состояние истории имеет приоритет.</small></label>
  {(!Number.isInteger(value.topK)||value.topK<1||value.topK>100||!Number.isInteger(value.tokenBudget)||value.tokenBudget<256||value.tokenBudget>8192)&&<small role="alert" className="error">Укажите от 1 до 100 воспоминаний и от 256 до 8192 токенов.</small>}
  <p role="status">{label}{data?.enabled&&<small> · обработано: {data.completed}, осталось: {data.queued+data.running}{data.failed>0?`, с ошибкой: ${data.failed}`:''}</small>}</p>
  {building&&Boolean(data?.servingRevision)&&<small className="muted">Пока новая память готовится, используется предыдущая версия.</small>}
  <button type="button" className="ghost" disabled={!saved.enabled||dirty||building||rebuild.isPending||status.isError||status.isPending} onClick={()=>rebuild.mutate()}>{rebuild.isPending?'Запускаю…':'Переиндексировать память'}</button>
  {dirty&&<small className="muted">Сначала сохраните настройки кнопкой ниже. Включение запустит подготовку памяти автоматически.</small>}
  {rebuild.isError&&<p className="error">Не удалось запустить переиндексацию. Попробуйте ещё раз.</p>}
 </fieldset>
}
