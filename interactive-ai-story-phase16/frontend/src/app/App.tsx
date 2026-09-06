import { Fragment, FormEvent, useEffect, useRef, useState } from 'react'
import { flushSync } from 'react-dom'
import { Navigate, Route, Routes, useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { addDirectorInstruction, applyDirectorExact, createActionRequestId, createStory, generateSetup, generationEventsUrl, getCurrent, getDirector, getDirectorHistory, getSaveLibrary, getSetup, startStory, submitAction, createSave, forkSave, previewSave, updateSaveCard, listAISettings, createAIRevision, activateAIRevision, getPromptStudio, createPromptRevision, activatePromptRevision, generateSceneImages, generateParagraphImage, listStories, deleteStory, type PromptRoleSettings, type StoryListItem } from '../lib/api'
import { applyGenerationUpdate, pendingTurnIsVisible, readPendingReaderGeneration, readerHasCoherentChoices, writePendingReaderGeneration, type GenerationUpdate, type PendingReaderGeneration } from '../lib/readerFlow'
import { paragraphCanBeIllustrated } from '../lib/illustrationPlacement'
import { SetupComponentEditor } from './SetupEditor'
import { SETUP_COMPONENT_ORDER, setupComponentDescription, setupComponentOrderIndex, setupComponentTitle } from './setupEditorModel'
import { SetupAssistant } from './SetupAssistant'
import { DirectorWorkspace } from './DirectorWorkspace'
import { Narrator, SpeakerIcon, type NarrationStartRequest } from './Narrator'
import '../styles/app.css'

function NewStory() {
  const nav=useNavigate(); const queryClient=useQueryClient(); const [title,setTitle]=useState(''); const [idea,setIdea]=useState(''); const [tags,setTags]=useState(''); const [createdStoryId,setCreatedStoryId]=useState(''); const [historySearch,setHistorySearch]=useState(''); const [showAllStories,setShowAllStories]=useState(false); const [deleteCandidate,setDeleteCandidate]=useState<StoryListItem|null>(null)
  const stories=useQuery({queryKey:['stories'],queryFn:listStories})
  const create=useMutation({mutationFn:async()=>{let storyId=createdStoryId;if(!storyId){const story=await createStory({title,idea,tags:tags.split(',').map(x=>x.trim()).filter(Boolean)});storyId=story.ID;setCreatedStoryId(storyId)}await generateSetup(storyId);return storyId},onSuccess:storyId=>nav(`/stories/${storyId}/setup`),onSettled:()=>queryClient.invalidateQueries({queryKey:['stories']})})
  const progress=useQuery({queryKey:['setup',createdStoryId,'generation-progress'],queryFn:()=>getSetup(createdStoryId),enabled:Boolean(createdStoryId)&&create.isPending,refetchInterval:create.isPending?1500:false})
  const retry=useMutation({mutationFn:async(storyId:string)=>{await generateSetup(storyId);return storyId},onSuccess:storyId=>nav(`/stories/${storyId}/setup`),onSettled:()=>queryClient.invalidateQueries({queryKey:['stories']})})
  const remove=useMutation({mutationFn:deleteStory,onSuccess:(_,storyId)=>{queryClient.setQueryData<{stories:StoryListItem[]}>(['stories'],current=>current?{stories:current.stories.filter(story=>story.id!==storyId)}:current);setDeleteCandidate(null)}})
  function submit(e:FormEvent){e.preventDefault();if(title.trim())create.mutate()}
  const openStory=(story:StoryListItem)=>story.latestTimeline?nav(`/reader/${story.latestTimeline.id}`):nav(`/stories/${story.id}/setup`)
  const date=(value:string)=>new Intl.DateTimeFormat('ru-RU',{day:'numeric',month:'short',year:'numeric'}).format(new Date(value))
  const storyItems=stories.data?.stories??[]
  const normalizedSearch=historySearch.trim().toLocaleLowerCase('ru-RU')
  const filteredStories=normalizedSearch?storyItems.filter(story=>`${story.title} ${story.description}`.toLocaleLowerCase('ru-RU').includes(normalizedSearch)):storyItems
  const visibleStories=showAllStories||normalizedSearch?filteredStories:filteredStories.slice(0,8)
  const ideaLength=Array.from(idea).length
  const generatedKeys=new Set((progress.data?.components??[]).map(component=>component.key))
  return <main className="home-screen">
    <header className="home-header"><div><p className="eyebrow">Interactive Story</p><strong>Ваши истории</strong></div><button className="ghost" type="button" onClick={()=>nav("/settings/ai")}>⚙ Настройки AI</button></header>
    <div className="home-layout">
      <section className="panel home-create">
        <p className="eyebrow">New Story</p><h1>Создать историю</h1><p className="muted">Задайте основу. AI подготовит структурированный черновик, который можно проверить, изменить и зафиксировать.</p>
        <form className="stack" onSubmit={submit}>
          <label>Название<input value={title} onChange={e=>setTitle(e.target.value)} required maxLength={120}/></label>
          <label>Идея<textarea value={idea} onChange={e=>setIdea(e.target.value)} rows={5} maxLength={40000}/><small className={`idea-counter ${ideaLength>12000?'warning':''}`}>{ideaLength.toLocaleString('ru-RU')} / 40 000{ideaLength>12000?' · для генерации AI использует компактную выдержку, оригинал сохранится полностью':''}</small></label>
          <label>Теги<input value={tags} onChange={e=>setTags(e.target.value)} placeholder="mystery, romance, modern"/></label>
          <button className="primary" disabled={create.isPending||ideaLength>40000}>{create.isPending?'Генерирую части начала…':createdStoryId?'Продолжить генерацию':'Создать и сгенерировать'}</button>
          {create.isPending&&<div className="setup-generation-progress" role="status"><strong>Подготовка сохраняется поэтапно</strong><div>{SETUP_COMPONENT_ORDER.map(key=><span className={generatedKeys.has(key)?'done':'pending'} key={key}>{generatedKeys.has(key)?'✓':'·'} {setupComponentTitle(key)}</span>)}</div><small>Квесты и начальная сцена состоят из нескольких связанных запросов. Страницу можно восстановить из сохранённого черновика.</small></div>}
          {create.error&&<div className="error setup-create-error" role="alert">Генерация остановилась на одном из этапов. Уже готовые разделы сохранены; повтор продолжит с недостающего раздела.<br/><small>{create.error.message}</small>{createdStoryId&&<button className="ghost compact" type="button" onClick={()=>nav(`/stories/${createdStoryId}/setup`)}>Открыть сохранённый черновик</button>}</div>}
        </form>
      </section>
      <section className="story-library" aria-labelledby="story-library-title">
        <header><div><p className="eyebrow">Продолжить</p><h2 id="story-library-title">Ранее созданные</h2></div><button className="ghost compact" type="button" onClick={()=>stories.refetch()} disabled={stories.isFetching} aria-label="Обновить список историй">{stories.isFetching?'Обновляю…':'↻ Обновить'}</button></header>
        {storyItems.length>0&&<label className="story-search"><span>Найти историю</span><input type="search" value={historySearch} onChange={event=>setHistorySearch(event.target.value)} placeholder="Название или описание"/></label>}
        {stories.isPending&&<div className="story-library-state">Загружаю истории…</div>}
        {stories.error&&<div className="error" role="alert">Не удалось загрузить истории.<br/><small>{stories.error.message}</small></div>}
        {stories.data&&storyItems.length===0&&<div className="story-library-state"><span>✦</span><strong>Здесь появятся ваши истории</strong><p className="muted tiny">Создайте первую историю слева — она сохранится на сервере.</p></div>}
        {storyItems.length>0&&filteredStories.length===0&&<div className="story-library-state"><strong>Ничего не найдено</strong><p className="muted tiny">Попробуйте другое название или часть описания.</p></div>}
        <div className="story-list">{visibleStories.map(story=>{
          const ready=story.readyComponents>=story.totalComponents
          const isRetrying=retry.isPending&&retry.variables===story.id
          return <article className="story-card" key={story.id}>
            <div className="story-card-head"><div><h3>{story.title}</h3><span>{date(story.updatedAt)}</span></div><span className={`story-state ${story.latestTimeline?'playing':ready?'ready':'draft'}`}>{story.latestTimeline?'Идёт история':ready?'Начало готово':`Черновик ${story.readyComponents}/${story.totalComponents}`}</span></div>
            {story.description&&<p>{story.description}</p>}
            {story.latestTimeline&&<div className="story-timeline"><span>Последняя линия</span><strong>{story.latestTimeline.name}</strong><small>{story.latestTimeline.headEventSeq} событий</small></div>}
            <div className="story-card-actions">
              {(story.latestTimeline||ready||story.readyComponents>0)&&<button className="primary" type="button" onClick={()=>openStory(story)}>{story.latestTimeline?'Продолжить историю':ready?'Открыть подготовку':'Открыть черновик'}</button>}
              {!story.latestTimeline&&!ready&&<button className={story.readyComponents>0?'ghost':'primary'} type="button" disabled={retry.isPending} onClick={()=>retry.mutate(story.id)}>{isRetrying?'Генерирую…':'Повторить генерацию'}</button>}
              <button className="ghost story-delete" type="button" onClick={()=>{remove.reset();setDeleteCandidate(story)}} aria-label={`Удалить историю «${story.title}»`}>Удалить</button>
            </div>
            {isRetrying&&<p className="muted tiny story-retry-note" role="status">Восстанавливаю начало. Это может занять несколько минут.</p>}
            {retry.error&&retry.variables===story.id&&<p className="error tiny" role="alert">Не удалось завершить генерацию: {retry.error.message}</p>}
          </article>
        })}</div>
        {!normalizedSearch&&filteredStories.length>8&&<button className="ghost story-show-all" type="button" onClick={()=>setShowAllStories(value=>!value)}>{showAllStories?'Показать только последние':`Показать ещё · ${filteredStories.length-8}`}</button>}
      </section>
    </div>
    {deleteCandidate&&<div className="confirm-sheet" role="presentation"><section className="confirm-card story-delete-dialog" role="dialog" aria-modal="true" aria-labelledby="delete-story-title"><p className="eyebrow">Удаление истории</p><h2 id="delete-story-title">Удалить «{deleteCandidate.title}»?</h2><p className="muted">История исчезнет с начального экрана. Это действие нельзя отменить через интерфейс.</p>{remove.error&&<p className="error" role="alert">Не удалось удалить историю: {remove.error.message}</p>}<div className="story-delete-dialog-actions"><button className="ghost" type="button" disabled={remove.isPending} onClick={()=>setDeleteCandidate(null)}>Отмена</button><button className="danger-button" type="button" disabled={remove.isPending} onClick={()=>remove.mutate(deleteCandidate.id)}>{remove.isPending?'Удаляю…':'Удалить историю'}</button></div></section></div>}
  </main>
}

function SetupReview(){
 const {storyId=''}=useParams();const nav=useNavigate();const queryClient=useQueryClient();const [selectedKey,setSelectedKey]=useState('story_bible');const [draftEpoch,setDraftEpoch]=useState(0)
 const resume=useMutation({mutationFn:()=>generateSetup(storyId),onSettled:()=>queryClient.invalidateQueries({queryKey:['setup',storyId]})})
 const q=useQuery({queryKey:['setup',storyId],queryFn:()=>getSetup(storyId),refetchInterval:resume.isPending?1500:false})
 const start=useMutation({mutationFn:()=>startStory(storyId),onSuccess:r=>nav(`/reader/${r.timeline.ID}`)})
 const components=[...(q.data?.components??[])].sort((a,b)=>setupComponentOrderIndex(a.key)-setupComponentOrderIndex(b.key))
 const selected=components.find(c=>c.key===selectedKey)??components[0]
 const ready=SETUP_COMPONENT_ORDER.every(key=>components.some(component=>component.key===key&&component.status==='ready'))
 useEffect(()=>{if(selected&&selected.key!==selectedKey)setSelectedKey(selected.key)},[selected,selectedKey])
 return <main className="setup-screen">
   <header className="setup-topbar"><div><p className="eyebrow">Story Setup</p><h1>Подготовка истории</h1><p className="muted">Редактируйте обычные поля или попросите AI предложить изменения. Каждый готовый раздел уже сохранён на сервере.</p></div><div className="setup-top-actions"><button className="ghost" type="button" onClick={()=>nav('/settings/prompts')}>Промпты</button>{!ready&&<button className="ghost" type="button" onClick={()=>resume.mutate()} disabled={resume.isPending}>{resume.isPending?'Продолжаю…':'Продолжить генерацию'}</button>}<button className="primary" onClick={()=>start.mutate()} disabled={!ready||q.isPending||start.isPending}>{start.isPending?'Запускаю…':'Начать историю'}</button></div></header>
   {q.error&&<p className="error setup-global-error">{q.error.message}</p>}
   {resume.error&&<p className="error setup-global-error">Генерация снова остановилась, но готовые разделы сохранены: {resume.error.message}</p>}
   {resume.isPending&&<p className="setup-notice setup-global-error" role="status">Продолжаю с первого недостающего раздела. Уже готовые разделы не перегенерируются.</p>}
   {start.error&&<p className="error setup-global-error">{start.error.message}</p>}
   <div className="setup-workspace">
     <nav className="setup-nav" aria-label="Разделы setup">{SETUP_COMPONENT_ORDER.map(key=>{const c=components.find(component=>component.key===key);return <button type="button" key={key} disabled={!c} className={`setup-nav-item ${selected?.key===key?'active':''} ${c?'':'missing'}`} onClick={()=>c&&setSelectedKey(key)}><span className="setup-nav-title">{setupComponentTitle(key)}</span><small>{setupComponentDescription(key)}</small><span className="setup-nav-state">{c?`${c.locked?'🔒 ':''}rev ${c.revision} · ${c.status}`:resume.isPending?'ожидает генерации':'не создан'}</span></button>})}</nav>
     <section className="setup-editor-slot">{selected?<SetupComponentEditor storyId={storyId} component={selected} draftEpoch={draftEpoch}/>:q.isPending?<div className="setup-loading">Загружаю setup…</div>:<div className="error">Setup пуст.</div>}</section>
     {components.length>0&&<SetupAssistant storyId={storyId} components={components} selectedKey={selected?.key??selectedKey} onSelectComponent={setSelectedKey} onDraftsApplied={()=>setDraftEpoch(value=>value+1)}/>}
   </div>
 </main>
}
function generationPhaseLabel(phase:string|null){
 return ({submitting:'Отправляю действие',queued:'Ожидаю ответа модели',interpreting:'Понимаю действие',directing:'Определяю развитие сцены',pacing:'Проверяю переход сцены и главы',writing:'Пишу продолжение',draft_ready:'Черновик готов',draft_replaced:'Черновик уточнён',finalizing:'Проверяю изменения мира и варианты действий',evaluating:'Параллельно обновляю мир, героя и квесты',world:'Обновляю персонажей и места',journal:'Обновляю способности и инвентарь',objectives:'Проверяю цели и достижения',choices:'Готовлю новые варианты действий',validating:'Проверяю результат',committing:'Сохраняю продолжение',syncing:'Обновляю сцену',retrying:'Ответ модели некорректен — повторяю попытку',reconnecting:'Восстанавливаю связь с генерацией',completed:'Обновляю сцену',failed:'Генерация завершилась ошибкой'} as Record<string,string>)[phase??'']??'Продолжаю историю'
}
function sentenceChunks(text:string){
 const matches=text.match(/[^.!?…]+(?:[.!?…]+[»”"'’)]*|$)/g)
 return (matches??[]).map(x=>x.trim()).filter(Boolean)
}
function splitParagraphs(text:string){
 const normalized=text.replace(/\r\n/g,'\n').trim()
 const explicit=normalized.split(/\n\s*\n/).map(x=>x.trim()).filter(Boolean)
 if(explicit.length>=4)return explicit
 const sentences=sentenceChunks(normalized)
 if(sentences.length<8)return explicit.length?explicit:[normalized]
 // Display-only fallback for local models that ignore requested blank lines.
 // Canon text is untouched; we only make the Reader readable.
 const target=Math.min(9,Math.max(7,Math.ceil(sentences.length/5)))
 const out:string[]=[]
 let cursor=0
 for(let i=0;i<target;i++){
  const remaining=sentences.length-cursor
  const groupsLeft=target-i
  const take=Math.max(1,Math.ceil(remaining/groupsLeft))
  out.push(sentences.slice(cursor,cursor+take).join(' '))
  cursor+=take
 }
 return out.filter(Boolean)
}
function ImageIcon(){
 return <svg className="paragraph-action-icon" viewBox="0 0 24 24" aria-hidden="true"><rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="8.5" cy="9" r="1.6"/><path d="m5 17 4.5-4 3.2 2.8 2.4-2.2L19 17"/></svg>
}
function IllustrationMoment({images,index}:{images:{id:string;status:string;sourceBeatId?:string;images:Array<{id:string;variant:number;url:string}>};index:number}){
 return <section className="illustration-moment inline-illustration" data-generation-id={images.id}>
  <header><span className="eyebrow">Иллюстрация {index+1}</span></header>
  <div className="illustration-variants">
   {(images.status==='pending'||images.status==='running')&&<><div className="scene-image-skeleton">Генерируется вариант 1…</div><div className="scene-image-skeleton">Генерируется вариант 2…</div></>}
   {images.status==='done'&&images.images.map((im,position)=><figure className="scene-image-card" key={im.id}><img src={im.url} alt={`Иллюстрация ${index+1}, изображение ${position+1}`}/></figure>)}
   {images.status==='failed'&&<div className="scene-image-failed"><p>Не удалось сгенерировать эту иллюстрацию.</p></div>}
  </div>
 </section>
}
function ObjectivePanel({objectives,abilities,attributes,inventory,currencies,heroStats,onCollapse}:{objectives:import('../lib/api').Objective[];abilities:import('../lib/api').JournalEntry[];attributes:import('../lib/api').JournalEntry[];inventory:import('../lib/api').JournalEntry[];currencies:import('../lib/api').JournalEntry[];heroStats:import('../lib/api').HeroStat[];onCollapse:()=>void}){
 const [tab,setTab]=useState<'quests'|'hero'>(objectives.length?'quests':'hero')
 if(objectives.length===0&&abilities.length===0&&attributes.length===0&&inventory.length===0&&currencies.length===0&&heroStats.length===0)return null
 const majors=objectives.filter(x=>x.scope==='global');const stages=objectives.filter(x=>x.scope==='minor');const knownMajorIds=new Set(majors.map(x=>x.id));const orphanStages=stages.filter(x=>!x.parentObjectiveId||!knownMajorIds.has(x.parentObjectiveId))
 const state=(x:import('../lib/api').Objective)=>x.status==='completed'?'✓ Выполнено':x.status==='failed'?'✕ Провалено':`${Math.max(0,Math.min(100,x.progress))}%`
 const stageLabel=(x:import('../lib/api').Objective)=>x.kind==='event'?'Событие':x.kind==='milestone'?'Рубеж':'Этап'
 const stage=(x:import('../lib/api').Objective)=><article className={`quest-stage ${x.status}`} key={x.id}><div className="objective-head"><span className="objective-kind">{stageLabel(x)}</span><span className="objective-state">{state(x)}</span></div><strong>{x.title}</strong>{x.description&&<p>{x.description}</p>}{x.status==='active'&&x.progress>0&&<div className="objective-progress" aria-label={`Прогресс ${x.progress}%`}><span style={{width:`${Math.max(0,Math.min(100,x.progress))}%`}}/></div>}{x.evidence&&<small>{x.evidence}</small>}</article>
 const quest=(x:import('../lib/api').Objective)=>{const type=x.questType??'main';const linked=stages.filter(stage=>stage.parentObjectiveId===x.id);const activeStages=linked.filter(stage=>stage.status==='active');const closedStages=linked.filter(stage=>stage.status!=='active');return <article className={`objective-card quest ${type} ${x.status}`} key={x.id}><div className="objective-head"><span className="objective-kind">{type==='side'?'Побочный квест':'Главный квест'}</span><span className="objective-state">{state(x)}</span></div><strong>{x.title}</strong>{x.description&&<p>{x.description}</p>}{x.status==='active'&&<div className="objective-progress" aria-label={`Прогресс квеста ${x.progress}%`}><span style={{width:`${Math.max(0,Math.min(100,x.progress))}%`}}/></div>}<div className="quest-stage-list">{activeStages.map(stage)}{activeStages.length===0&&closedStages.length===0&&<small className="quest-empty">Следующий этап появится по мере развития сюжета.</small>}{closedStages.length>0&&<details className="quest-stage-history"><summary>Пройденные этапы · {closedStages.length}</summary>{closedStages.map(stage)}</details>}</div>{x.evidence&&<small>{x.evidence}</small>}</article>}
 const activeMajors=majors.filter(x=>x.status==='active');const mainQuests=activeMajors.filter(x=>(x.questType??'main')==='main');const sideQuests=activeMajors.filter(x=>x.questType==='side');const closedMajors=majors.filter(x=>x.status!=='active');const activeStageCount=stages.filter(x=>x.status==='active').length
 const journalCard=(x:import('../lib/api').JournalEntry)=><article className={`hero-journal-card ${x.category} ${x.status}`} key={x.id}><div className="objective-head"><span className="objective-kind">{x.category==='ability'?(x.level||'Способность'):x.category==='attribute'?(x.level||'Характеристика'):x.category==='currency'?'Баланс':`Количество · ${x.quantity??1}`}</span><span className="objective-state">{x.status==='active'?'Активно':'Выбыло'}</span></div><strong>{x.category==='currency'?`${Number(x.quantity??0).toLocaleString('ru-RU')} ${x.name}`:x.name}</strong>{x.description&&<p>{x.description}</p>}{x.tags&&x.tags.length>0&&<div className="journal-tags">{x.tags.map(tag=><span key={tag}>{tag}</span>)}</div>}{x.evidence&&<small>{x.evidence}</small>}</article>
 const activeAbilities=abilities.filter(x=>x.status==='active');const oldAbilities=abilities.filter(x=>x.status!=='active');const activeAttributes=attributes.filter(x=>x.status==='active');const oldAttributes=attributes.filter(x=>x.status!=='active');const activeInventory=inventory.filter(x=>x.status==='active');const oldInventory=inventory.filter(x=>x.status!=='active');const activeCurrencies=currencies.filter(x=>x.status==='active');const oldCurrencies=currencies.filter(x=>x.status!=='active')
 const heroCount=activeAbilities.length+activeAttributes.length+activeInventory.length+activeCurrencies.length+heroStats.length
 const statValue=(value:unknown)=>typeof value==='boolean'?(value?'Да':'Нет'):Array.isArray(value)?value.join(', '):value==null?'—':String(value)
 return <section className="objective-panel" role="region" aria-label="Журнал истории"><header><div><p className="eyebrow">Canon героя</p><h2>{tab==='quests'?'Квесты':'Герой'}</h2></div><button className="objective-collapse" type="button" onClick={onCollapse} aria-label="Скрыть журнал" title="Скрыть журнал">←</button></header><div className="journal-tabs" role="tablist" aria-label="Раздел журнала"><button type="button" role="tab" aria-selected={tab==='quests'} className={tab==='quests'?'active':''} onClick={()=>setTab('quests')}>Квесты <b>{activeMajors.length}</b></button><button type="button" role="tab" aria-selected={tab==='hero'} className={tab==='hero'?'active':''} onClick={()=>setTab('hero')}>Герой <b>{heroCount}</b></button></div>{tab==='quests'?<><div className="objective-panel-summary"><span className="active-chip">{activeMajors.length} активных</span><span>{activeStageCount} текущих этапов</span></div>{mainQuests.length>0&&<section className="quest-section"><h3>Главные квесты</h3><div className="objective-list">{mainQuests.map(quest)}</div></section>}{sideQuests.length>0&&<section className="quest-section side"><h3>Побочные квесты</h3><div className="objective-list">{sideQuests.map(quest)}</div></section>}{activeMajors.length===0&&<p className="journal-empty">Активных квестов пока нет.</p>}{orphanStages.length>0&&<details className="objective-history"><summary>Задачи старых историй · {orphanStages.length}</summary><div className="quest-stage-list">{orphanStages.map(stage)}</div></details>}{closedMajors.length>0&&<details className="objective-history"><summary>Завершённые квесты · {closedMajors.length}</summary><div className="objective-list">{closedMajors.map(quest)}</div></details>}</>:<div className="hero-journal"><section><h3>Деньги и характеристики</h3>{activeCurrencies.length>0&&<div className="hero-journal-list money-list">{activeCurrencies.map(journalCard)}</div>}{activeAttributes.length>0&&<div className="hero-stats">{activeAttributes.map(attribute=><div key={attribute.id}><span>{attribute.name}</span><strong>{attribute.level||'—'}</strong></div>)}</div>}{heroStats.length>0&&<div className="hero-stats">{heroStats.map(stat=><div key={stat.key}><span>{stat.key}</span><strong>{statValue(stat.value)}</strong></div>)}</div>}{activeCurrencies.length===0&&activeAttributes.length===0&&heroStats.length===0&&<p className="journal-empty">Баланс и характеристики появятся после явного события в истории.</p>}</section><section><h3>Способности</h3><div className="hero-journal-list">{activeAbilities.map(journalCard)}{activeAbilities.length===0&&<p className="journal-empty">Проявленные способности появятся по ходу истории.</p>}</div></section><section><h3>Инвентарь</h3><div className="hero-journal-list">{activeInventory.map(journalCard)}{activeInventory.length===0&&<p className="journal-empty">Значимые предметы появятся после получения героем.</p>}</div></section>{oldAbilities.length+oldAttributes.length+oldInventory.length+oldCurrencies.length>0&&<details className="objective-history"><summary>Утрачено или использовано · {oldAbilities.length+oldAttributes.length+oldInventory.length+oldCurrencies.length}</summary><div className="hero-journal-list">{[...oldAbilities,...oldAttributes,...oldInventory,...oldCurrencies].map(journalCard)}</div></details>}</div>}</section>
}
function objectivePanelPreference(timelineId:string){
 try{
  const saved=localStorage.getItem(`reader-objectives:${timelineId}`)
  if(saved)return saved!=='closed'
  return typeof matchMedia!=='function'||!matchMedia('(max-width: 960px)').matches
 }catch{return true}
}
function Reader(){
 const {timelineId=''}=useParams();const nav=useNavigate()
 const [customAction,setCustomAction]=useState('')
 const [pendingGeneration,setPendingGeneration]=useState<PendingReaderGeneration|null>(()=>readPendingReaderGeneration(timelineId))
 const [objectivesOpen,setObjectivesOpen]=useState(()=>objectivePanelPreference(timelineId))
 const [generationError,setGenerationError]=useState('')
 const [narrationStart,setNarrationStart]=useState<NarrationStartRequest|null>(null)
 const [narrationCurrent,setNarrationCurrent]=useState<string|null>(null)
 const generationSubmissionLocked=useRef(false)
 // SSE drives active generation. Snapshot polling is only a bounded recovery
 // net: faster while reconciling a turn, rare while the Reader is idle.
 const q=useQuery({queryKey:['current',timelineId],queryFn:()=>getCurrent(timelineId),refetchInterval:pendingGeneration?5000:60000})
 const action=useMutation({
  mutationFn:(submission:{text:string;expectedHead:number;requestId:string})=>submitAction(timelineId,submission.expectedHead,submission.text,submission.requestId),
  onError:(error)=>{setGenerationError(error.message);setPendingGeneration(null)},
  onSuccess:(r,submission)=>{
   if(customAction.trim()===submission.text.trim())setCustomAction('')
   setPendingGeneration(current=>current?{...current,generationId:r.generationId,phase:'queued'}:current)
  }
 })
 const regenerate=useMutation({mutationFn:()=>generateSceneImages(q.data!.storyId,q.data!.sceneId),onSuccess:()=>q.refetch()})
 const paragraphImage=useMutation({mutationFn:(target:{beatId:string;paragraph:number})=>generateParagraphImage(q.data!.storyId,q.data!.sceneId,target.beatId,target.paragraph),onSuccess:()=>q.refetch()})
 useEffect(()=>{setPendingGeneration(readPendingReaderGeneration(timelineId))},[timelineId])
 useEffect(()=>{setObjectivesOpen(objectivePanelPreference(timelineId))},[timelineId])
 useEffect(()=>{try{localStorage.setItem(`reader-objectives:${timelineId}`,objectivesOpen?'open':'closed')}catch{/* Persistence is optional. */}},[timelineId,objectivesOpen])
 useEffect(()=>{if(!objectivesOpen)return;const close=(event:KeyboardEvent)=>{if(event.key==='Escape')setObjectivesOpen(false)};window.addEventListener('keydown',close);return()=>window.removeEventListener('keydown',close)},[objectivesOpen])
 useEffect(()=>{writePendingReaderGeneration(timelineId,pendingGeneration)},[timelineId,pendingGeneration])
 useEffect(()=>{if(!pendingGeneration)generationSubmissionLocked.current=false},[pendingGeneration])
 useEffect(()=>{
  if(!pendingGeneration||pendingGeneration.generationId||!pendingGeneration.requestId||!pendingGeneration.actionText||action.isPending)return
  action.mutate({text:pendingGeneration.actionText,expectedHead:pendingGeneration.fromHeadEventSeq,requestId:pendingGeneration.requestId})
 // A persisted submission without a generation id is resumed with the same
 // idempotency key after reload. The backend returns the original job.
 // eslint-disable-next-line react-hooks/exhaustive-deps
 },[pendingGeneration?.generationId,pendingGeneration?.requestId,pendingGeneration?.actionText,pendingGeneration?.fromHeadEventSeq,action.isPending])
 useEffect(()=>{if(!q.data)return;const key=`reader-anchor:${timelineId}`;const raw=sessionStorage.getItem(key);if(!raw)return;try{const a=JSON.parse(raw) as {beatId?:string;offset?:number};requestAnimationFrame(()=>{const anchor=a.beatId?document.getElementById(`beat-${a.beatId}`):null;if(anchor)anchor.scrollIntoView({block:'start'});window.scrollBy(0,Number(a.offset??0));sessionStorage.removeItem(key)})}catch{sessionStorage.removeItem(key)}},[q.data,timelineId])
 useEffect(()=>{
  const generationId=pendingGeneration?.generationId
  if(!generationId)return
  const stream=new EventSource(generationEventsUrl(generationId))
  stream.addEventListener('generation',(e)=>{
   const u=JSON.parse((e as MessageEvent).data) as GenerationUpdate
   if(u.phase==='failed'){
    stream.close();setGenerationError(u.error?.trim()||'Генерация завершилась ошибкой. Можно повторить действие.');setPendingGeneration(null);void q.refetch();return
   }
   if(u.phase==='completed'){
    // Completion means the worker committed. Controls stay locked until the
    // Reader returns one coherent snapshot: new beat + matching choice set.
    stream.close();setPendingGeneration(current=>current?{...current,phase:'syncing'}:current);void q.refetch();return
   }
   setPendingGeneration(current=>applyGenerationUpdate(timelineId,current,u))
  })
  stream.onerror=()=>{setPendingGeneration(current=>current?{...current,phase:'reconnecting'}:current);void q.refetch()}
  return ()=>stream.close()
 // Reconnect only when the durable generation id changes. q.refetch is stable
 // for the lifetime of this query and must not cause SSE reconnects per render.
 // eslint-disable-next-line react-hooks/exhaustive-deps
 },[pendingGeneration?.generationId])
 useEffect(()=>{
  if(!pendingGeneration||!q.data)return
  if(!pendingTurnIsVisible(q.data,pendingGeneration))return
  setPendingGeneration(null)
  action.reset()
 // eslint-disable-next-line react-hooks/exhaustive-deps
 },[q.data?.beatId,q.data?.headEventSeq,q.data?.choiceSet?.beatId,q.data?.choiceSet?.choices.length,pendingGeneration?.fromBeatId,pendingGeneration?.fromHeadEventSeq])
 function sendAction(text:string){
  const value=text.trim();if(!value||generationSubmissionLocked.current||pendingGeneration||action.isPending||!q.data||!readerHasCoherentChoices(q.data))return
  generationSubmissionLocked.current=true
  action.reset()
  setGenerationError('')
  const pending={requestId:createActionRequestId(),actionText:value,fromBeatId:q.data.beatId,fromHeadEventSeq:q.data.headEventSeq,phase:'submitting',createdAt:Date.now()}
  writePendingReaderGeneration(timelineId,pending)
  // Render and disable the controls before a second click/Enter event can be
  // delivered. The recovery effect above starts the idempotent POST.
  flushSync(()=>setPendingGeneration(pending))
 }
 if(q.isPending)return <main className="page"><p>Загрузка…</p></main>
 if(q.error)return <main className="page"><p className="error">{q.error.message}</p></main>
 const v=q.data!;const allGenerations=(v.imageGenerations?.length?v.imageGenerations:(v.imageGeneration?[v.imageGeneration]:[]));const generations=allGenerations.filter(g=>g.sceneId===v.sceneId);const choicesReady=readerHasCoherentChoices(v);const busy=Boolean(pendingGeneration)||action.isPending||!choicesReady
 const visibleChoices=choicesReady?v.choiceSet!.choices:[]
 const imageBusy=generations.some(g=>g.status==='pending'||g.status==='running')
 const beats=(v.beats?.length?v.beats:[{id:v.beatId,position:1,text:v.text}])
 const narrationParagraphs=beats.flatMap((beat,beatIndex)=>(beat.paragraphs?.length?beat.paragraphs:splitParagraphs(beat.text)).map((text,paragraphIndex)=>({key:`${beat.id}:${paragraphIndex+1}`,label:`Фрагмент ${beatIndex+1} · абзац ${paragraphIndex+1}`,text})))
 const objectives=v.objectives??[];const abilities=v.abilities??[];const attributes=v.attributes??[];const inventory=v.inventory??[];const currencies=v.currencies??[];const heroStats=v.heroStats??[];const activeObjectiveCount=objectives.filter(x=>x.status==='active'&&!x.parentObjectiveId&&x.scope==='global').length;const activeHeroCount=[...abilities,...attributes,...inventory,...currencies].filter(x=>x.status==='active').length+heroStats.length;const journalAvailable=objectives.length+abilities.length+attributes.length+inventory.length+currencies.length+heroStats.length>0
 const latestBeatId=beats[beats.length-1]?.id??v.beatId
 const generationsByBeat=new Map<string,typeof generations>()
 for(const g of generations){const key=g.sourceBeatId||latestBeatId;const list=generationsByBeat.get(key)??[];list.push(g);generationsByBeat.set(key,list)}
 let illustrationIndex=0
 return <main className="reader"><div className={`reader-layout ${!journalAvailable?'objectives-empty':objectivesOpen?'objectives-open':'objectives-closed'}`}>
  {journalAvailable&&objectivesOpen&&<><button className="objective-backdrop" type="button" aria-label="Закрыть журнал" onClick={()=>setObjectivesOpen(false)}/><aside className="objective-sidebar"><ObjectivePanel objectives={objectives} abilities={abilities} attributes={attributes} inventory={inventory} currencies={currencies} heroStats={heroStats} onCollapse={()=>setObjectivesOpen(false)}/></aside></>}
  {journalAvailable&&!objectivesOpen&&<button className="objective-rail-toggle" type="button" onClick={()=>setObjectivesOpen(true)} aria-expanded="false" aria-label={`Показать журнал: ${activeObjectiveCount} квестов, ${activeHeroCount} записей героя`}><span>◎</span><strong>Журнал</strong><b>{activeObjectiveCount+activeHeroCount}</b></button>}
 <section className="reader-card">
  <div className="reader-toolbar"><div className="reader-location"><p className="eyebrow">Глава {v.chapterNumber} · сцена {v.sceneNumber}</p><span>{v.chapterTitle}</span></div><div className="reader-toolbar-actions"><button className="ghost" onClick={()=>{sessionStorage.setItem(`reader-anchor:${timelineId}`,JSON.stringify({beatId:v.beatId,offset:window.scrollY}));location.href=`/director/${timelineId}`}}>🎬 <span>Режиссёр</span></button><button className="ghost" onClick={()=>nav(`/stories/${v.storyId}/saves?timeline=${timelineId}`)}>💾 <span>Сохранения</span></button></div></div>
  <h1 className="reader-title">{v.sceneGoal||'История'}</h1>
  <section className="scene-prose" aria-label="Текст текущей сцены">
   {beats.map(beat=>{
    const paragraphs=(beat.paragraphs?.length?beat.paragraphs:splitParagraphs(beat.text))
    const attached=(generationsByBeat.get(beat.id)??[]).sort((a,b)=>(a.momentIndex??1)-(b.momentIndex??1))
    const slots=new Map<number,typeof attached>()
    const occupiedAnchors=new Set(attached.filter(images=>images.status!=='failed').map(images=>images.anchorParagraph).filter((value):value is number=>Boolean(value)))
    attached.forEach((images,i)=>{
     // New generations carry a semantic paragraph anchor chosen by the Visual
     // Director. Legacy rows without one keep the previous even-spacing fallback.
     const fallback=Math.round(((i+1)*paragraphs.length)/(attached.length+1))
     const requested=images.anchorParagraph??0
     const position=Math.max(1,Math.min(paragraphs.length,requested>0?requested:fallback))
     const list=slots.get(position)??[];list.push(images);slots.set(position,list)
    })
    return <article className="scene-beat" id={`beat-${beat.id}`} key={beat.id}>
     <div className="beat-text">
      {paragraphs.map((paragraph,i)=>{return <Fragment key={`${beat.id}-p-${i}`}>
       <div className={`illustratable-paragraph ${narrationCurrent===`${beat.id}:${i+1}`?'narration-active':''}`}><p>{paragraph}</p><div className="paragraph-actions"><button className="paragraph-read" type="button" title="Читать отсюда" aria-label={`Читать вслух с абзаца ${i+1}`} onClick={()=>setNarrationStart({key:`${beat.id}:${i+1}`,nonce:Date.now()})}><SpeakerIcon/></button>{paragraphCanBeIllustrated(paragraph)&&!occupiedAnchors.has(i+1)&&<button className="paragraph-illustrate" type="button" title="Создать иллюстрацию" aria-label={`Создать иллюстрацию для абзаца ${i+1}`} disabled={paragraphImage.isPending} onClick={()=>paragraphImage.mutate({beatId:beat.id,paragraph:i+1})}><ImageIcon/></button>}</div></div>
       {(slots.get(i+1)??[]).map(images=><IllustrationMoment key={images.id} images={images} index={illustrationIndex++}/>)}
      </Fragment>})}
     </div>
    </article>
   })}
  </section>
  {pendingGeneration?.provisionalText&&<section className="provisional-beat" aria-label="Предварительный текст продолжения"><span>Черновик · ещё не сохранён в истории</span>{splitParagraphs(pendingGeneration.provisionalText).map((paragraph,index)=><p key={`provisional-${pendingGeneration.provisionalRevision??0}-${index}`}>{paragraph}</p>)}</section>}
  {generations.length===0&&<div className="scene-image-empty"><p>{regenerate.isPending?'Ставлю иллюстрацию сцены в очередь…':'Иллюстрация подготавливается в фоновой очереди.'}</p></div>}
  <div className="scene-image-regenerate"><button className="ghost" onClick={()=>regenerate.mutate()} disabled={regenerate.isPending||imageBusy}>{regenerate.isPending?'Ищу новый момент…':'Найти следующий момент для иллюстрации · 2 варианта'}</button></div>
  {regenerate.error&&<p className="error scene-image-error">{regenerate.error.message}</p>}
  {paragraphImage.error&&<p className="error scene-image-error">{paragraphImage.error.message}</p>}
  {busy
   ? <section className="generation-wait" aria-live="polite" aria-busy="true">
      <div className="generation-status"><span className="generation-spinner" aria-hidden="true"/><div><strong role="status">{generationPhaseLabel(pendingGeneration?.phase??'syncing')}…</strong><small>Повторная отправка заблокирована до результата.</small></div></div>
      {pendingGeneration?.actionText&&<p className="generation-action"><span>Выбрано:</span> {pendingGeneration.actionText}</p>}
      <p className="muted tiny">Новые варианты появятся только вместе с согласованным продолжением истории.</p>
     </section>
   : <>
      <h2 className="choice-heading">Что вы сделаете?</h2>
      <div className="choices">{visibleChoices.map((x,i)=><button key={`${i}-${x}`} onClick={()=>sendAction(x)}>{x}</button>)}</div>
      <form className="custom-action" onSubmit={e=>{e.preventDefault();sendAction(customAction)}}>
       <label>Свой вариант действия<textarea rows={3} value={customAction} onChange={e=>setCustomAction(e.target.value)} onKeyDown={e=>{if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();sendAction(customAction)}}} placeholder="Опишите, что ваш персонаж пытается сделать…"/></label>
       <button className="primary" type="submit" disabled={!customAction.trim()}>Отправить свой вариант</button>
      </form>
     </>}
  {generationError&&<p className="error" role="alert">{generationError}<br/><small>Действие не применено — его можно отправить повторно.</small></p>}
 </section>
 <aside className="narrator-aside"><Narrator timelineId={timelineId} paragraphs={narrationParagraphs} requestedStart={narrationStart} onCurrentChange={setNarrationCurrent}/></aside>
 </div></main>
}

function SaveLibraryScreen(){
 const {storyId=''}=useParams();const nav=useNavigate();const qc=useQueryClient();const params=new URLSearchParams(location.search);const active=params.get('timeline')??''
 const q=useQuery({queryKey:['save-library',storyId],queryFn:()=>getSaveLibrary(storyId)})
 const [name,setName]=useState('');const [branchName,setBranchName]=useState('');const [selected,setSelected]=useState<string|null>(null)
 const create=useMutation({mutationFn:()=>createSave(active,name||`Сохранение ${new Date().toLocaleString()}`,'',false),onSuccess:()=>{setName('');qc.invalidateQueries({queryKey:['save-library',storyId]})}})
 const fork=useMutation({mutationFn:(saveId:string)=>forkSave(saveId,branchName.trim()),onSuccess:t=>nav(`/reader/${t.id}`)})
 const preview=useMutation({mutationFn:(saveId:string)=>previewSave(saveId)})
 if(q.isPending)return <main className="page"><p>Загрузка сохранений…</p></main>
 if(q.error)return <main className="page"><p className="error">{q.error.message}</p></main>
 const lib=q.data
 return <main className="save-screen"><header className="director-header"><button className="ghost" onClick={()=>nav(active?`/reader/${active}`:'/')}>← Назад</button><strong>Сохранения и ветки</strong></header>
 <section className="save-content">
  <div className="save-create"><input value={name} onChange={e=>setName(e.target.value)} placeholder="Название сохранения"/><button className="primary" disabled={!active||create.isPending} onClick={()=>create.mutate()}>Сохранить сейчас</button></div>
  <div className="timeline-list">{lib.timelines.map(t=><section className={`timeline-node ${t.id===active?'active':''}`} key={t.id}>
    <header><div><span className="eyebrow">{t.parentTimelineId?'Ветка':'Основная линия'}</span><h2>{t.name}</h2></div><button className="ghost" onClick={()=>nav(`/reader/${t.id}`)}>Открыть</button></header>
    {t.parentTimelineId&&<p className="muted tiny">Ответвление от {lib.timelines.find(x=>x.id===t.parentTimelineId)?.name??'другой линии'}</p>}
    <div className="save-cards">{(lib.saves[t.id]??[]).map(save=><article className="save-card" key={save.id}>
      <div className="thumb" aria-label={`thumbnail ${save.thumbnailStatus}`}>{save.thumbnailStatus==='ready'?'▣':'◇'}</div>
      <div className="save-meta"><strong>{save.displaySlot?`Слот ${save.displaySlot} · `:''}{save.name}</strong><span className="muted tiny">Beat {save.eventSeq} · {new Date(save.createdAt).toLocaleString()}</span></div>
      <div className="save-actions"><button onClick={()=>{setSelected(save.id);preview.mutate(save.id)}}>Просмотр</button><button onClick={()=>updateSaveCard(save.id,save.displaySlot,!save.pinned).then(()=>qc.invalidateQueries({queryKey:['save-library',storyId]}))}>{save.pinned?'★':'☆'}</button></div>
    </article>)}</div>
  </section>)}</div>
  {selected&&preview.data&&<div className="confirm-sheet" role="dialog" aria-modal="true"><div className="confirm-card"><h2>Загрузить как новую ветку?</h2><p className="muted">Исходная Timeline останется неизменной. Это безопасный Fork из выбранного SavePoint.</p><input value={branchName} onChange={e=>setBranchName(e.target.value)} placeholder="Название новой ветки"/><div className="actions"><button onClick={()=>setSelected(null)}>Отмена</button><button className="primary" disabled={!branchName.trim()||fork.isPending} onClick={()=>fork.mutate(selected)}>Создать ветку</button></div></div></div>}
 </section></main>
}

const googleStoryModels=[
 {id:'gemini-3.6-flash',label:'Gemini 3.6 Flash',description:'Более сильная универсальная Flash-модель для сюжета и планирования.'},
 {id:'antigravity-preview-05-2026',label:'Antigravity',description:'Управляемый агент Preview. Работает заметно медленнее и расходует больше токенов.'},
 {id:'gemini-3.1-flash-lite',label:'Gemini 3.1 Flash-Lite',description:'Быстрая и экономичная модель; Google рекомендует переходить с неё на 3.5 Flash-Lite до мая 2027 года.'},
 {id:'gemini-3.5-flash-lite',label:'Gemini 3.5 Flash-Lite',description:'Самая быстрая и экономичная модель семейства 3.5.'},
 {id:'gemma-4-31b-it',label:'Gemma 4 31B',description:'Открытая модель Gemma 4, размещённая в Gemini API.'}
] as const

function AISettings(){
 const nav=useNavigate();const qc=useQueryClient();const q=useQuery({queryKey:['ai-settings'],queryFn:listAISettings})
 const active=q.data?.revisions.find(x=>x.active)
 const [storyProvider,setStoryProvider]=useState('openai_compatible');const [storyModel,setStoryModel]=useState('');const [storyEndpoint,setStoryEndpoint]=useState('');const [safeHybrid,setSafeHybrid]=useState(false);const [apiKeysText,setApiKeysText]=useState('');const [showApiKey,setShowApiKey]=useState(false)
 const [imageProvider,setImageProvider]=useState('manual');const [imageModel,setImageModel]=useState('manual-v1')
 useEffect(()=>{if(!active)return;setStoryProvider(active.storyLlm.provider);setStoryModel(active.storyLlm.model);setStoryEndpoint(active.public.storyLlmEndpoint);setSafeHybrid(Boolean(active.public.storyLlmSafeHybrid));setImageProvider(active.image.provider);setImageModel(active.image.model)},[active])
 const googleAI=storyProvider==='google_gemini'
 const selectedGoogleModel=googleStoryModels.find(model=>model.id===storyModel)??googleStoryModels[3]
 const chooseProvider=(provider:string)=>{setStoryProvider(provider);setSafeHybrid(false);setApiKeysText('');setShowApiKey(false);if(provider==='google_gemini'){setStoryModel('gemini-3.5-flash-lite');setStoryEndpoint('https://generativelanguage.googleapis.com/v1beta/openai')}else{setStoryModel(active?.storyLlm.provider==='openai_compatible'?active.storyLlm.model:'');setStoryEndpoint(active?.storyLlm.provider==='openai_compatible'?active.public.storyLlmEndpoint:'http://127.0.0.1:8081')}}
 const toggleSafeHybrid=()=>setSafeHybrid(value=>{const next=!value;if(next)setStoryModel('antigravity-preview-05-2026');return next})
 const enteredKeys=apiKeysText.split(/[\n,]+/).map(value=>value.trim()).filter((value,index,all)=>Boolean(value)&&all.indexOf(value)===index)
 const keyRequired=googleAI&&!(active?.storyLlm.provider==='google_gemini'&&active.storyLlmSecretConfigured)&&enteredKeys.length===0
 const create=useMutation({mutationFn:()=>createAIRevision({config:{
   storyLlm:{kind:'story_llm',provider:storyProvider,model:storyModel,profile:safeHybrid?'google-ai-safe-hybrid':googleAI?'google-ai-rotating':active?.storyLlm.provider==='openai_compatible'?active.storyLlm.profile:'llamacpp-local'},
   embedding:active?.embedding??{kind:'embedding',provider:'local_sentence_transformers',model:'deepvk/USER2-small',profile:'cpu'},
   image:{kind:'image',provider:imageProvider,model:imageModel,profile:active?.image.profile||''},
   public:{storyLlmEndpoint:storyEndpoint,storyLlmContext:googleAI?1048576:active?.public.storyLlmContext||16384,storyLlmKvDType:googleAI?'managed':active?.public.storyLlmKvDType||'q8_0',storyLlmSafeHybrid:googleAI&&safeHybrid,worldRulesSafeMode:false,embeddingEndpoint:active?.public.embeddingEndpoint||'',imageEndpoint:active?.public.imageEndpoint||''}
 },storyLlmApiKeys:enteredKeys.length?enteredKeys:undefined,activate:true}),onSuccess:()=>{setApiKeysText('');qc.invalidateQueries({queryKey:['ai-settings']})}})
 const activate=useMutation({mutationFn:activateAIRevision,onSuccess:()=>qc.invalidateQueries({queryKey:['ai-settings']})})
 return <main className="settings-screen"><header className="director-header"><button className="ghost" onClick={()=>nav(-1)}>← Назад</button><strong>AI Providers</strong></header><section className="settings-content">
  {q.isPending&&<p>Загрузка…</p>}{q.error&&<p className="error">{q.error.message}</p>}
  {active&&<section className="settings-card"><p className="eyebrow">Active revision {active.revision}</p><h1>Провайдеры</h1><div className="settings-tabs"><span className="active-chip">Провайдеры</span><button className="ghost" type="button" onClick={()=>nav("/settings/prompts")}>Prompt Studio</button></div><p className="muted">Изменение создаёт новую immutable revision. Уже запущенные jobs и старые изображения сохраняют прежнюю provenance.</p>
   <div className="stack">
    <fieldset className="provider-picker"><legend>Провайдер текста и сюжета</legend><button type="button" className={`provider-option ${!googleAI?'selected':''}`} aria-pressed={!googleAI} onClick={()=>chooseProvider('openai_compatible')}><span className="provider-radio"/><span><strong>Локальная / OpenAI-compatible</strong><small>Текущий llama.cpp или другой совместимый сервер</small></span></button><button type="button" className={`provider-option ${googleAI?'selected':''}`} aria-pressed={googleAI} onClick={()=>chooseProvider('google_gemini')}><span className="provider-radio"/><span><strong>Google AI</strong><small>Gemini, Gemma или управляемый агент Antigravity</small></span></button></fieldset>
    {googleAI?<div className="gemini-settings">
     <div className={`safe-hybrid-card ${safeHybrid?'enabled':''}`}>
      <div><strong>Безопасный гибрид</strong><small>Flash-Lite анализирует действие, темп и варианты. Antigravity пишет сцену и контролирует мир, журнал и квесты.</small></div>
      <button className="settings-switch" type="button" role="switch" aria-label="Безопасный гибрид" aria-checked={safeHybrid} onClick={toggleSafeHybrid}><span/></button>
     </div>
     {safeHybrid?<div className="hybrid-routing"><span><b>Быстрые роли</b>Gemini 3.5 Flash-Lite</span><span><b>Основные роли</b>Antigravity</span></div>:<label>Модель<select value={storyModel} onChange={e=>setStoryModel(e.target.value)}>{googleStoryModels.map(model=><option value={model.id} key={model.id}>{model.label}</option>)}</select><small className="muted">{selectedGoogleModel.description}</small></label>}
     <div className="provider-facts"><span>Режим</span><strong>{safeHybrid?'Безопасный гибрид':selectedGoogleModel.label}</strong><span>API</span><code>{safeHybrid?'Gemini API + Interactions API':storyModel==='antigravity-preview-05-2026'?'Google Interactions API':'Google AI Gemini API'}</code></div>
     {!safeHybrid&&storyModel==='antigravity-preview-05-2026'&&<p className="provider-notice"><strong>Antigravity — не обычная LLM.</strong> Для каждого запроса запускается управляемый агент в удалённой среде Google. Продолжения могут появляться заметно дольше и расходовать больше квоты.</p>}
     {safeHybrid&&<p className="provider-notice hybrid-notice"><strong>Канон остаётся под контролем Antigravity.</strong> Flash-Lite не изменяет состояние мира, инвентарь или прогресс квестов. По тестам этот режим примерно в 1,7 раза быстрее чистого Antigravity.</p>}
     <p className="provider-notice"><strong>Бесплатный tier имеет квоты.</strong> Доступность и лимиты различаются по моделям; при исчерпании квоты backend последовательно пробует следующий сохранённый ключ.</p><a className="provider-key-link" href="https://aistudio.google.com/app/apikey" target="_blank" rel="noreferrer">Получить API key в Google AI Studio ↗</a>
    </div>:<><label>Story LLM model<input value={storyModel} onChange={e=>setStoryModel(e.target.value)} placeholder="Имя модели llama.cpp"/></label><label>Story LLM endpoint<input value={storyEndpoint} onChange={e=>setStoryEndpoint(e.target.value)} placeholder="http://127.0.0.1:8081"/></label></>}
    <label>{googleAI?'Google AI API keys':'API keys'} <span className="muted tiny">{active.storyLlm.provider===storyProvider&&active.storyLlmSecretConfigured?`сохранено ключей: ${active.storyLlmSecretCount||1} · значения скрыты`:keyRequired?'нужно указать для включения Google AI':googleAI?'не настроены':'необязательно'}</span><span className="secret-input"><textarea rows={googleAI?5:2} className={showApiKey?'secret-multiline':'secret-multiline masked'} value={apiKeysText} onChange={e=>setApiKeysText(e.target.value)} autoComplete="off" spellCheck={false} placeholder={active.storyLlm.provider===storyProvider&&active.storyLlmSecretConfigured?'оставьте пустым, чтобы сохранить текущий набор':googleAI?'по одному ключу на строку':'необязательно'}/><button className="ghost" type="button" onClick={()=>setShowApiKey(value=>!value)} aria-label={showApiKey?'Скрыть API keys':'Показать API keys'}>{showApiKey?'Скрыть':'Показать'}</button></span><small className="muted">Ключи хранятся только на компьютере с backend и используются по очереди. В PostgreSQL и обратно в браузер они не возвращаются.</small></label>
    <label>Image provider<input value={imageProvider} onChange={e=>setImageProvider(e.target.value)}/></label>
    <label>Image model<input value={imageModel} onChange={e=>setImageModel(e.target.value)}/></label>
    <button className="primary" disabled={!storyModel.trim()||(googleAI&&!storyEndpoint.trim())||keyRequired||create.isPending} onClick={()=>create.mutate()}>{create.isPending?'Сохраняю…':safeHybrid?'Сохранить и включить безопасный гибрид':googleAI?'Сохранить и включить выбранную модель':'Сохранить и включить локальную модель'}</button>
    {keyRequired&&<p className="muted tiny">Чтобы включить Google AI, вставьте API key из Google AI Studio.</p>}
    {create.error&&<p className="error">{create.error.message}</p>}
   </div>
  </section>}
  <section className="settings-card"><h2>История revisions</h2><div className="revision-list">{q.data?.revisions.map(v=><article className="revision-row" key={v.id}><div><strong>rev {v.revision} · {v.public.storyLlmSafeHybrid?'Безопасный гибрид':v.storyLlm.model}</strong><p className="muted tiny">{v.storyLlm.provider}{v.public.storyLlmSafeHybrid?' · Flash-Lite + Antigravity':''}{v.storyLlmSecretCount?` · ключей: ${v.storyLlmSecretCount}`:''} · image: {v.image.provider}/{v.image.model}</p></div>{v.active?<span className="active-chip">active</span>:<button className="ghost" disabled={activate.isPending} onClick={()=>activate.mutate(v.id)}>Активировать</button>}</article>)}</div>{activate.error&&<p className="error">{activate.error.message}</p>}</section>
 </section></main>
}


const promptRoles=[
 ['story_setup','Legacy Story Setup'],['setup_architect','Setup Architect'],['setup_assistant','Legacy Setup Assistant'],['setup_editor','Context Setup Editor'],['director_editor','Live Director Editor'],['action_interpreter','Action Interpreter'],['director','Director'],['pacing','Scene and Chapter Pacing'],['writer','Writer'],['world_evaluator','Live World Evaluator'],['state_evaluator','Hero Journal Evaluator'],['objectives','Legacy Objectives'],['quest_evaluator','Quest Evaluator'],['choices','Choices'],['image_prompt','Image Prompt'],['image_moments','Image Moments'],['image_next_moment','Next Illustration'],['image_paragraph','Paragraph Illustration'],['structured_repair','Structured Repair']
] as const

function clonePromptSettings(value:Record<string,PromptRoleSettings>):Record<string,PromptRoleSettings>{return Object.fromEntries(Object.entries(value).map(([k,v])=>[k,{...v}]))}

function PromptStudio(){
 const nav=useNavigate();const qc=useQueryClient();const q=useQuery({queryKey:['prompt-studio'],queryFn:getPromptStudio})
 const [role,setRole]=useState<string>('writer');const [prompts,setPrompts]=useState<Record<string,string>>({});const [settings,setSettings]=useState<Record<string,PromptRoleSettings>>({});const [dirty,setDirty]=useState(false)
 useEffect(()=>{if(!q.data?.active)return;setPrompts({...q.data.active.prompts});setSettings(clonePromptSettings(q.data.active.roleSettings));setDirty(false)},[q.data?.active])
 const create=useMutation({mutationFn:()=>createPromptRevision({prompts,roleSettings:settings,activate:true}),onSuccess:()=>{setDirty(false);qc.invalidateQueries({queryKey:['prompt-studio']})}})
 const activate=useMutation({mutationFn:activatePromptRevision,onSuccess:()=>qc.invalidateQueries({queryKey:['prompt-studio']})})
 const current=settings[role]??{temperature:0.2,topP:0.8,maxTokens:2048}
 const setCurrent=(patch:Partial<PromptRoleSettings>)=>{setSettings(prev=>({...prev,[role]:{...(prev[role]??current),...patch}}));setDirty(true)}
 const resetRole=()=>{if(!q.data)return;setPrompts(prev=>({...prev,[role]:q.data!.builtin.prompts[role]??''}));setSettings(prev=>({...prev,[role]:{...(q.data!.builtin.roleSettings[role]??current)}}));setDirty(true)}
 const resetAll=()=>{if(!q.data)return;setPrompts({...q.data.builtin.prompts});setSettings(clonePromptSettings(q.data.builtin.roleSettings));setDirty(true)}
 const valid=promptRoles.every(([key])=>{const v=settings[key];return Boolean(prompts[key]?.trim())&&Boolean(v&&v.topP>0&&v.topP<=1&&v.temperature>=0&&v.temperature<=2&&v.maxTokens>=128)})
 return <main className="settings-screen"><header className="director-header"><button className="ghost" onClick={()=>nav('/settings/ai')}>← Провайдеры</button><strong>Prompt Studio</strong><span className={dirty?'prompt-dirty':'active-chip'}>{dirty?'есть изменения':q.data?`active rev ${q.data.active.revision}`:'...'}</span></header>
  <section className="prompt-studio-content">
   {q.isPending&&<p>Загрузка…</p>}{q.error&&<p className="error">{q.error.message}</p>}
   {q.data&&<>
    <aside className="prompt-role-list" aria-label="Prompt roles">{promptRoles.map(([key,label])=><button type="button" key={key} className={role===key?'prompt-role active':'prompt-role'} onClick={()=>setRole(key)}><span>{label}</span><small>{key}</small></button>)}</aside>
    <section className="prompt-editor-card">
     <div className="prompt-editor-head"><div><p className="eyebrow">Runtime prompt</p><h1>{promptRoles.find(([key])=>key===role)?.[1]}</h1><p className="muted tiny">Сохранение создаёт новую immutable revision. Уже поставленные в очередь генерации продолжают использовать закреплённую старую revision.</p></div><button className="ghost" type="button" onClick={resetRole}>Вернуть builtin для роли</button></div>
     <label className="prompt-source-label">System prompt<textarea className="prompt-source" value={prompts[role]??''} onChange={e=>{setPrompts(prev=>({...prev,[role]:e.target.value}));setDirty(true)}} spellCheck={false}/></label>
     <div className="sampling-grid">
      <label>Temperature<input type="number" min="0" max="2" step="0.05" value={current.temperature} onChange={e=>setCurrent({temperature:Number(e.target.value)})}/></label>
      <label>Top P<input type="number" min="0.01" max="1" step="0.05" value={current.topP} onChange={e=>setCurrent({topP:Number(e.target.value)})}/></label>
      <label>Max tokens<input type="number" min="128" max="32768" step="128" value={current.maxTokens} onChange={e=>setCurrent({maxTokens:Number(e.target.value)})}/></label>
     </div>
     <div className="prompt-savebar"><button className="ghost" type="button" onClick={resetAll}>Вернуть все builtin</button><button className="primary" disabled={!dirty||!valid||create.isPending} onClick={()=>create.mutate()}>Сохранить и активировать revision</button></div>
     {create.error&&<p className="error">{create.error.message}</p>}
    </section>
    <section className="prompt-history settings-card"><h2>История prompt revisions</h2><p className="muted tiny">Можно мгновенно вернуть любой старый набор. Активация влияет только на новые запросы и jobs.</p><div className="revision-list">{q.data.revisions.map(v=><article className="revision-row" key={v.id}><div><strong>Prompt rev {v.revision}</strong><p className="muted tiny">{Object.keys(v.prompts).length} ролей · Writer {v.roleSettings.writer?.temperature ?? '—'} / top_p {v.roleSettings.writer?.topP ?? '—'}</p></div>{v.active?<span className="active-chip">active</span>:<button className="ghost" disabled={activate.isPending} onClick={()=>activate.mutate(v.id)}>Активировать</button>}</article>)}</div></section>
   </>}
  </section>
 </main>
}

function DirectorQuestTree({objectives}:{objectives:Array<Record<string,unknown>>}){
 const majors=objectives.filter(x=>x.scope==='global');const stages=objectives.filter(x=>x.scope==='minor');const majorIds=new Set(majors.map(x=>String(x.id??'')));const orphans=stages.filter(x=>!majorIds.has(String(x.parentObjectiveId??'')))
 const stageLabel=(x:Record<string,unknown>)=>x.kind==='event'?'Событие':x.kind==='milestone'?'Рубеж':'Этап'
 const status=(x:Record<string,unknown>)=>x.status==='completed'?'✓ выполнено':x.status==='failed'?'✕ провалено':`${Number(x.progress??0)}%`
 const renderStage=(x:Record<string,unknown>,i:number)=><div className={`director-quest-stage ${String(x.status??'active')}`} key={String(x.id??i)}><div><span className="objective-kind">{stageLabel(x)}</span><span className="objective-state">{status(x)}</span></div><strong>{String(x.title??'Этап')}</strong>{Boolean(x.successCriteria)&&<small>Условие: {String(x.successCriteria)}</small>}{Boolean(x.evidence)&&<small>Основание: {String(x.evidence)}</small>}</div>
 const renderQuest=(quest:Record<string,unknown>,i:number)=>{const type=String(quest.questType??'main');const linked=stages.filter(x=>String(x.parentObjectiveId??'')===String(quest.id??''));return <article className={`director-objective quest ${type} ${String(quest.status??'active')}`} key={String(quest.id??i)}><div><span className="objective-kind">{type==='side'?'Побочный квест':'Главный квест'}</span><span className="objective-state">{status(quest)}</span></div><strong>{String(quest.title??'Квест')}</strong>{Boolean(quest.successCriteria)&&<small>Финальное условие: {String(quest.successCriteria)}</small>}<div className="director-quest-stages">{linked.map(renderStage)}{linked.length===0&&<small className="muted">Этапы ещё не сформированы.</small>}</div></article>}
 const main=majors.filter(x=>String(x.questType??'main')==='main');const side=majors.filter(x=>String(x.questType??'main')==='side')
 return <div className="director-quest-list">{main.length>0&&<section className="quest-section"><h3>Главные квесты</h3>{main.map(renderQuest)}</section>}{side.length>0&&<section className="quest-section side"><h3>Побочные квесты</h3>{side.map(renderQuest)}</section>}{orphans.length>0&&<details className="objective-history"><summary>Несвязанные задачи старых историй · {orphans.length}</summary>{orphans.map(renderStage)}</details>}</div>
}

export function DirectorLegacy(){
 const {timelineId=''}=useParams();const nav=useNavigate();const qc=useQueryClient()
 const q=useQuery({queryKey:['director',timelineId],queryFn:()=>getDirector(timelineId)})
 const history=useQuery({queryKey:['director-history',timelineId],queryFn:()=>getDirectorHistory(timelineId)})
 const [instruction,setInstruction]=useState('');const [priority,setPriority]=useState('normal')
 const instruct=useMutation({mutationFn:()=>addDirectorInstruction(timelineId,instruction,'next_beat',priority),onSuccess:()=>{setInstruction('');qc.invalidateQueries({queryKey:['director',timelineId]})}})
 const applyStat=useMutation({mutationFn:(args:{ownerId:string;key:string;value:number})=>applyDirectorExact(timelineId,q.data?.semanticRevision??1,'set_stat',{OwnerType:'character',OwnerID:args.ownerId,Key:args.key,ValueType:'number',Value:args.value,EvolutionMode:'immediate'}),onSuccess:()=>qc.invalidateQueries({queryKey:['director',timelineId]})})
 const renderJournalEntry=(x:Record<string,unknown>,i:number)=>{const category=String(x.category??'');return <article className={`director-journal-entry ${String(x.status??'active')}`} key={String(x.id??i)}><div><strong>{category==='currency'?`${Number(x.quantity??0).toLocaleString('ru-RU')} ${String(x.name??'денег')}`:String(x.name??'Запись')}</strong><span className="objective-state">{category==='item'?`×${Number(x.quantity??1)}`:category==='currency'?'Баланс':String(x.level??'освоено')}</span></div>{Boolean(x.description)&&<p>{String(x.description)}</p>}{Boolean(x.evidence)&&<small>Последнее изменение: {String(x.evidence)}</small>}</article>}
 return <main className="director-screen"><header className="director-header"><button className="ghost" onClick={()=>nav(`/reader/${timelineId}`)}>← Назад к истории</button><strong>Режиссёр</strong></header>
   <section className="director-content">
    {q.isPending&&<p>Загрузка…</p>}{q.error&&<p className="error">{q.error.message}</p>}
    {q.data&&<>
      <div className="director-grid">
       <section className="director-card director-objectives"><h2>Сюжетные квесты и этапы</h2>{(q.data.objectives??[]).length===0?<p className="muted">Квесты появятся из setup или по развитию сюжета.</p>:<DirectorQuestTree objectives={q.data.objectives??[]}/>}</section>
       <section className="director-card"><h2>Персонажи</h2>{(q.data.characters??[]).map((c,i)=><div className="entity-row" key={i}><span>{String(c.name??'Персонаж')}</span><span className="muted">{String(c.mood??'')}</span></div>)}</section>
       <section className="director-card"><h2>Отношения</h2>{(q.data.relationships??[]).map((x,i)=><div className="entity-row" key={i}><span>{String(x.status??'relation')}</span><span className="muted">{String(x.summary??'')}</span></div>)}</section>
       <section className="director-card"><h2>Статы</h2>{(q.data.stats??[]).slice(0,12).map((x,i)=><div className="entity-row stat-row" key={i}><span>{String(x.key)}</span><input aria-label={`stat-${i}`} type="number" defaultValue={Number(x.value??0)} onBlur={e=>applyStat.mutate({ownerId:String(x.ownerId),key:String(x.key),value:Number(e.target.value)})}/></div>)}</section>
       <section className="director-card"><h2>Характеристики героя</h2>{(q.data.attributes??[]).length===0?<p className="muted">Характеристики пока не зафиксированы.</p>:<div className="director-journal-list">{(q.data.attributes??[]).map(renderJournalEntry)}</div>}</section>
       <section className="director-card"><h2>Способности героя</h2>{(q.data.abilities??[]).length===0?<p className="muted">Проявленные способности пока не зафиксированы.</p>:<div className="director-journal-list">{(q.data.abilities??[]).map(renderJournalEntry)}</div>}</section>
       <section className="director-card"><h2>Деньги и инвентарь героя</h2>{(q.data.inventory??[]).length===0?<p className="muted">Баланс и значимые предметы пока не зафиксированы.</p>:<div className="director-journal-list">{(q.data.inventory??[]).map(renderJournalEntry)}</div>}{(q.data.items??[]).length>0&&<details className="objective-history"><summary>Технические состояния предметов · {(q.data.items??[]).length}</summary>{(q.data.items??[]).map((x,i)=><div className="entity-row" key={i}><span>{String(x.name??'Предмет')}</span><span className="muted">{String(x.condition??'')}</span></div>)}</details>}</section>
       <section className="director-card"><h2>Facts / Knowledge / Beliefs</h2><p>Facts: {(q.data.facts??[]).length}</p><p>Knowledge: {(q.data.knowledge??[]).length}</p><p>Beliefs: {(q.data.beliefs??[]).length}</p></section>
       <section className="director-card"><h2>Направление истории</h2><textarea rows={5} value={instruction} onChange={e=>setInstruction(e.target.value)} placeholder="Инструкция для следующего хода"/><select value={priority} onChange={e=>setPriority(e.target.value)}><option value="normal">Normal</option><option value="high">High</option><option value="hard">Hard</option></select><button className="primary" disabled={!instruction.trim()||instruct.isPending} onClick={()=>instruct.mutate()}>Применить инструкцию</button></section>
       <section className="director-card"><h2>История изменений</h2>{(history.data?.entries??[]).slice(0,8).map((x,i)=><div className="entity-row" key={i}><span>{String(x.commandType??'change')}</span><span className="muted">{String(x.targetType??'')}</span></div>)}</section>
      </div>
    </>}
   </section>
 </main>
}

export function App(){return <Routes><Route path="/" element={<NewStory/>}/><Route path="/stories/:storyId/setup" element={<SetupReview/>}/><Route path="/reader/:timelineId" element={<Reader/>}/><Route path="/director/:timelineId" element={<DirectorWorkspace/>}/><Route path="/stories/:storyId/saves" element={<SaveLibraryScreen/>}/><Route path="/settings/ai" element={<AISettings/>}/><Route path="/settings/prompts" element={<PromptStudio/>}/><Route path="*" element={<Navigate to="/" replace/>}/></Routes>}
