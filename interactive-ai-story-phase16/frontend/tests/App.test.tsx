import { afterEach, expect, test, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import { App } from '../src/app/App'

afterEach(()=>cleanup())

test('renders new story form with mobile-safe primary flow', () => {
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 render(<BrowserRouter><QueryClientProvider client={client}><App/></QueryClientProvider></BrowserRouter>)
 expect(screen.getByRole('heading',{name:'Создать историю'})).toBeInTheDocument()
 expect(screen.getByRole('button',{name:'Создать и сгенерировать'})).toBeInTheDocument()
 expect(screen.getByLabelText('Название')).toBeInTheDocument()
})

test('new story generation retry reuses the already created story', async () => {
 history.pushState({},'', '/')
 let createCalls=0;let generateCalls=0
 const fetchMock=vi.fn(async (input:RequestInfo|URL,init?:RequestInit) => {
  const url=String(input)
  if(url.endsWith('/api/v1/stories')&&init?.method==='POST'){
   createCalls++
   return new Response(JSON.stringify({ID:'story-retry',Title:'Retry story',Description:''}),{status:201,headers:{'Content-Type':'application/json'}})
  }
  if(url.endsWith('/story-retry/setup/generate')){
   generateCalls++
   if(generateCalls===1)return new Response('ai provider returned invalid output',{status:422})
   return new Response(JSON.stringify({components:[]}),{status:200,headers:{'Content-Type':'application/json'}})
  }
  return new Response(JSON.stringify({components:[]}),{status:200,headers:{'Content-Type':'application/json'}})
 })
 vi.stubGlobal('fetch',fetchMock)
 const client=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 const view=render(<BrowserRouter><QueryClientProvider client={client}><App/></QueryClientProvider></BrowserRouter>)
 fireEvent.change(view.getByLabelText('Название'),{target:{value:'Retry story'}})
 fireEvent.click(view.getByRole('button',{name:'Создать и сгенерировать'}))
 const retry=await view.findByRole('button',{name:'Продолжить генерацию'})
 expect(view.getByRole('alert')).toHaveTextContent('Уже готовые разделы сохранены')
 fireEvent.click(retry)
 await waitFor(()=>expect(generateCalls).toBe(2))
 expect(createCalls).toBe(1)
 vi.unstubAllGlobals();history.pushState({},'', '/')
})

test('AI settings persist the safe hybrid role routing', async () => {
 history.pushState({},'', '/settings/ai')
 const active={id:'revision-15',revision:15,storyLlm:{kind:'story_llm',provider:'google_gemini',model:'antigravity-preview-05-2026',profile:'google-ai-rotating'},embedding:{kind:'embedding',provider:'local_sentence_transformers',model:'deepvk/USER2-small',profile:'cpu'},image:{kind:'image',provider:'perchance_browser',model:'perchance-v1',profile:''},public:{storyLlmEndpoint:'https://generativelanguage.googleapis.com/v1beta/openai',storyLlmContext:1048576,storyLlmKvDType:'managed',storyLlmSafeHybrid:false,embeddingEndpoint:'',imageEndpoint:''},active:true,storyLlmSecretConfigured:true,storyLlmSecretCount:5}
 let submitted:Record<string,unknown>|undefined
 vi.stubGlobal('fetch',vi.fn(async (input:RequestInfo|URL,init?:RequestInit)=>{
  const url=String(input)
  if(url.endsWith('/api/v1/settings/ai/revisions')&&init?.method==='POST'){
   submitted=JSON.parse(String(init.body)) as Record<string,unknown>
   return new Response(JSON.stringify({...active,id:'revision-16',revision:16,public:{...active.public,storyLlmSafeHybrid:true}}),{status:201,headers:{'Content-Type':'application/json'}})
  }
  return new Response(JSON.stringify({revisions:[active]}),{status:200,headers:{'Content-Type':'application/json'}})
 }))
 const client=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 render(<BrowserRouter><QueryClientProvider client={client}><App/></QueryClientProvider></BrowserRouter>)
 const toggle=await screen.findByRole('switch',{name:'Безопасный гибрид'})
 expect(toggle).toHaveAttribute('aria-checked','false')
 fireEvent.click(toggle)
 expect(toggle).toHaveAttribute('aria-checked','true')
 fireEvent.click(screen.getByRole('button',{name:'Сохранить и включить безопасный гибрид'}))
 await waitFor(()=>expect(submitted).toBeDefined())
 expect(submitted).toMatchObject({config:{storyLlm:{provider:'google_gemini',model:'antigravity-preview-05-2026',profile:'google-ai-safe-hybrid'},public:{storyLlmSafeHybrid:true}}})
 vi.unstubAllGlobals();history.pushState({},'', '/')
})

test('story deletion requires confirmation and removes the card after success', async () => {
 history.pushState({},'', '/')
 let deleteCalls=0
 const story={id:'00000000-0000-4000-8000-000000000009',title:'История для удаления',description:'Черновик',status:'active',readyComponents:0,totalComponents:7,createdAt:'2026-08-23T10:00:00Z',updatedAt:'2026-08-23T10:00:00Z'}
 vi.stubGlobal('fetch',vi.fn(async (input:RequestInfo|URL,init?:RequestInit)=>{
  const url=String(input)
  if(url.endsWith(`/api/v1/stories/${story.id}`)&&init?.method==='DELETE'){
   deleteCalls++
   return new Response(null,{status:204})
  }
  if(url.endsWith('/api/v1/stories'))return new Response(JSON.stringify({stories:[story]}),{status:200,headers:{'Content-Type':'application/json'}})
  return new Response('{}',{status:200,headers:{'Content-Type':'application/json'}})
 }))
 const client=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 render(<BrowserRouter><QueryClientProvider client={client}><App/></QueryClientProvider></BrowserRouter>)
 const remove=await screen.findByRole('button',{name:'Удалить историю «История для удаления»'})
 fireEvent.click(remove)
 expect(deleteCalls).toBe(0)
 expect(screen.getByRole('dialog',{name:'Удалить «История для удаления»?'})).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:'Удалить историю'}))
 await waitFor(()=>expect(deleteCalls).toBe(1))
 await waitFor(()=>expect(screen.queryByText('История для удаления')).not.toBeInTheDocument())
 vi.unstubAllGlobals();history.pushState({},'', '/')
})

test('director survives legacy null collections and renders objectives', async () => {
 history.pushState({},'', '/director/9b2fa757-93a4-4fd7-95d0-1a8911f6b83c')
 vi.stubGlobal('fetch', vi.fn(async (input:RequestInfo|URL) => {
  const url=String(input)
  const body=url.endsWith('/history')
   ? {entries:null}
   : {timelineId:'9b2fa757-93a4-4fd7-95d0-1a8911f6b83c',semanticRevision:1,headEventSeq:4,characters:null,relationships:null,stats:null,items:null,facts:null,knowledge:null,beliefs:null,threads:null,instructions:null,objectives:[{id:'goal-1',scope:'global',kind:'quest',title:'Спасти город',successCriteria:'Город в безопасности',status:'active',progress:25},{id:'stage-1',parentObjectiveId:'goal-1',scope:'minor',kind:'task',title:'Открыть северные ворота',successCriteria:'Ворота открыты',status:'active',progress:10}]}
  return new Response(JSON.stringify(body),{status:200,headers:{'Content-Type':'application/json'}})
 }))
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 render(<BrowserRouter><QueryClientProvider client={client}><App/></QueryClientProvider></BrowserRouter>)
 expect(await screen.findByRole('heading',{name:'Квесты и этапы'})).toBeInTheDocument()
 expect(screen.getAllByText('Спасти город').length).toBeGreaterThan(0)
 expect(screen.getByText('Открыть северные ворота')).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:/Персонажи/}))
 expect(screen.getByRole('heading',{name:'Персонажи',level:1})).toBeInTheDocument()
 vi.unstubAllGlobals()
 history.pushState({},'', '/')
})

test('director edits saved directions and the complete hero state', async () => {
 const timelineId='9b2fa757-93a4-4fd7-95d0-1a8911f6b83e'
 history.pushState({},'',`/director/${timelineId}`)
 const view={timelineId,semanticRevision:7,headEventSeq:12,characters:[{id:'hero-1',kind:'player',name:'Алекс',mood:'насторожен',goal:'найти выход',active:true}],locations:[],relationships:[],stats:[{ownerType:'character',ownerId:'hero-1',key:'Выносливость',valueType:'number',value:8}],items:[],facts:[],knowledge:[],beliefs:[],threads:[],objectives:[],abilities:[{id:'ability-1',category:'ability',name:'Эхо',description:'Слышит следы магии',level:'начальный',status:'active',evidence:'Проявилось в лесу',tags:['магия']}],attributes:[],inventory:[{id:'coins-1',category:'currency',name:'кроны',description:'Монеты',quantity:45,status:'active',evidence:'Стартовый запас',tags:[]}],instructions:[{id:'instruction-1',timelineId,text:'Вести героя к башне',scope:'next_beat',priority:'normal',status:'expired',createdAtSeq:4}]}
 vi.stubGlobal('fetch',vi.fn(async (input:RequestInfo|URL)=>new Response(JSON.stringify(String(input).endsWith('/history')?{entries:[]}:view),{status:200,headers:{'Content-Type':'application/json'}})))
 const client=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 render(<BrowserRouter><QueryClientProvider client={client}><App/></QueryClientProvider></BrowserRouter>)
 await screen.findByRole('heading',{name:'Квесты и этапы'})
 fireEvent.click(screen.getByRole('button',{name:/Герой/}))
 expect(screen.getByRole('heading',{name:'Герой',level:1})).toBeInTheDocument()
 expect(screen.getByDisplayValue('Алекс')).toBeInTheDocument()
 expect(screen.getByRole('button',{name:'Сохранить состояние'})).toBeInTheDocument()
 expect(screen.getByRole('button',{name:'Сохранить запись'})).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:/кроны/}))
 expect(screen.getByDisplayValue('45')).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:/Направление/}))
 expect(screen.getByRole('heading',{name:'Направление истории'})).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:/Вести героя к башне/}))
 expect(screen.getByDisplayValue('Вести героя к башне')).toBeInTheDocument()
 expect(screen.getByRole('button',{name:'Сохранить и активировать снова'})).toBeInTheDocument()
 vi.unstubAllGlobals();history.pushState({},'', '/')
})

test('reader journal shows hero state, collapses and remembers its state', async () => {
 const timelineId='9b2fa757-93a4-4fd7-95d0-1a8911f6b83c'
 localStorage.clear();history.pushState({},'',`/reader/${timelineId}`)
 const current={timelineId,storyId:'story-1',headEventSeq:6,chapterTitle:'Глава первая',chapterGoal:'Спасти город',sceneId:'scene-1',sceneGoal:'Найти проводника',beatId:'beat-1',text:'История начинается.',beats:[{id:'beat-1',position:1,text:'История начинается.',paragraphs:['История начинается.']}],choices:['A','B','C','D'],choiceSet:{beatId:'beat-1',choices:['A','B','C','D']},objectives:[{id:'goal-1',scope:'global',title:'Спасти город',successCriteria:'Город в безопасности',status:'active',progress:25}],abilities:[{id:'ability-1',category:'ability',name:'Эхо-память',level:'нестабильно',status:'active',evidence:'Проявилась в коридоре'}],attributes:[{id:'attribute-1',category:'attribute',name:'Контроль маны',level:'начальный',status:'active',evidence:'Удержал нестабильное плетение'}],inventory:[{id:'item-1',category:'item',name:'Архивный ключ',quantity:1,status:'active',evidence:'Поднят у двери'}],currencies:[{id:'currency-1',category:'currency',name:'кредитов',quantity:120,status:'active',evidence:'Баланс подтверждён терминалом'}],heroStats:[{key:'Выносливость',valueType:'number',value:7}],imageGenerations:[{id:'images-1',sceneId:'scene-1',momentIndex:1,status:'failed',images:[]}]}
 vi.stubGlobal('fetch',vi.fn(async()=>new Response(JSON.stringify(current),{status:200,headers:{'Content-Type':'application/json'}})))
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 render(<BrowserRouter><QueryClientProvider client={client}><App/></QueryClientProvider></BrowserRouter>)
 expect(await screen.findByRole('region',{name:'Журнал истории'})).toBeInTheDocument()
 fireEvent.click(screen.getByRole('tab',{name:/Герой/}))
 expect(screen.getByText('120 кредитов')).toBeInTheDocument()
 expect(screen.getByText('Выносливость')).toBeInTheDocument()
 expect(screen.getByText('Контроль маны')).toBeInTheDocument()
 expect(screen.getByText('Эхо-память')).toBeInTheDocument()
 expect(screen.getByText('Архивный ключ')).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:'Скрыть журнал'}))
 expect(screen.queryByRole('region',{name:'Журнал истории'})).not.toBeInTheDocument()
 const reopen=screen.getByRole('button',{name:'Показать журнал: 1 квестов, 5 записей героя'})
 expect(localStorage.getItem(`reader-objectives:${timelineId}`)).toBe('closed')
 fireEvent.click(reopen)
 expect(screen.getByRole('region',{name:'Журнал истории'})).toBeInTheDocument()
 vi.unstubAllGlobals();history.pushState({},'', '/')
})

test('reader offers on-demand illustration for visual prose but not dialogue', async () => {
 const timelineId='9b2fa757-93a4-4fd7-95d0-1a8911f6b83d'
 history.pushState({},'',`/reader/${timelineId}`)
 const visual='Над затопленной площадью медленно поднялся огромный механический маяк, и холодный синий свет отразился в окнах, воде и лицах замерших у колоннады людей.'
 const dialogue='— За перевалом уже горят огни, и если мы поспешим, то успеем пройти через ворота до полуночи, — сказал проводник.'
 const current={timelineId,storyId:'story-1',headEventSeq:6,chapterTitle:'Глава первая',chapterGoal:'Добраться до башни',sceneId:'scene-1',sceneGoal:'Открыть ворота',beatId:'beat-1',text:`${visual}\n\n${dialogue}`,beats:[{id:'beat-1',position:1,text:`${visual}\n\n${dialogue}`,paragraphs:[visual,dialogue]}],choices:['A','B','C','D'],choiceSet:{beatId:'beat-1',choices:['A','B','C','D']},objectives:[],imageGenerations:[{id:'images-1',sceneId:'scene-1',sourceBeatId:'beat-1',momentIndex:1,anchorParagraph:2,status:'done',selectedImageId:'image-1',images:[{id:'image-1',variant:1,url:'/one.png'},{id:'image-2',variant:2,url:'/two.png'}]}]}
 const fetchMock=vi.fn(async (input:RequestInfo|URL) => {
  const url=String(input)
  if(url.endsWith('/images/paragraph'))return new Response(JSON.stringify({id:'new-image',sceneId:'scene-1',status:'pending',attempts:0,images:[]}),{status:202,headers:{'Content-Type':'application/json'}})
  return new Response(JSON.stringify(current),{status:200,headers:{'Content-Type':'application/json'}})
 })
 vi.stubGlobal('fetch',fetchMock)
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 render(<BrowserRouter><QueryClientProvider client={client}><App/></QueryClientProvider></BrowserRouter>)
 const button=await screen.findByRole('button',{name:'Создать иллюстрацию для абзаца 1'})
 expect(screen.queryByRole('button',{name:'Создать иллюстрацию для абзаца 2'})).not.toBeInTheDocument()
 expect(screen.getAllByRole('img',{name:/Иллюстрация 1, изображение/})).toHaveLength(2)
 expect(screen.queryByRole('button',{name:/Выбрать вариант|Выбрано/})).not.toBeInTheDocument()
 fireEvent.click(button)
 await waitFor(()=>expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining('/images/paragraph'),expect.objectContaining({method:'POST',body:JSON.stringify({beatId:'beat-1',paragraph:1})})))
 vi.unstubAllGlobals();history.pushState({},'', '/')
})

test('reader shows waiting immediately, suppresses duplicate submits, and unlocks only after failure', async () => {
 const timelineId='9b2fa757-93a4-4fd7-95d0-1a8911f6b83c'
 sessionStorage.clear();history.pushState({},'',`/reader/${timelineId}`)
 const current={timelineId,storyId:'story-1',headEventSeq:6,chapterTitle:'Глава первая',chapterGoal:'Добраться до башни',sceneId:'scene-1',sceneGoal:'Открыть ворота',beatId:'beat-1',text:'История начинается.',beats:[{id:'beat-1',position:1,text:'История начинается.',paragraphs:['История начинается.']}],choices:['A','B','C','D'],choiceSet:{beatId:'beat-1',choices:['A','B','C','D']},objectives:[],imageGenerations:[{id:'images-1',sceneId:'scene-1',momentIndex:1,status:'failed',images:[]}]}
 let resolveFirstAction:(response:Response)=>void=()=>{}
 const firstAction=new Promise<Response>(resolve=>{resolveFirstAction=resolve})
 let actionCalls=0
 const fetchMock=vi.fn(async (input:RequestInfo|URL,init?:RequestInit) => {
  const url=String(input)
  if(url.endsWith(`/api/v1/timelines/${timelineId}/actions`)&&init?.method==='POST'){
   actionCalls++
   if(actionCalls===1)return firstAction
   return new Response(JSON.stringify({generationId:'generation-2'}),{status:202,headers:{'Content-Type':'application/json'}})
  }
  return new Response(JSON.stringify(current),{status:200,headers:{'Content-Type':'application/json'}})
 })
 type Listener=(event:MessageEvent)=>void
 class FakeEventSource {
  static instances:FakeEventSource[]=[]
  listeners:Record<string,Listener[]>={}
  onerror:(()=>void)|null=null
  constructor(public url:string){FakeEventSource.instances.push(this)}
  addEventListener(name:string,listener:EventListener){(this.listeners[name]??=[]).push(listener as Listener)}
  close(){}
  emit(phase:string,error?:string){for(const listener of this.listeners.generation??[])listener(new MessageEvent('generation',{data:JSON.stringify({phase,error})}))}
 }
 vi.stubGlobal('fetch',fetchMock);vi.stubGlobal('EventSource',FakeEventSource)
 const client=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 render(<BrowserRouter><QueryClientProvider client={client}><App/></QueryClientProvider></BrowserRouter>)
 const choice=await screen.findByRole('button',{name:'A'})
 fireEvent.click(choice)
 fireEvent.click(choice)
 expect(screen.getByRole('status')).toHaveTextContent('Отправляю действие')
 expect(screen.queryByRole('button',{name:'A'})).not.toBeInTheDocument()
 await waitFor(()=>expect(actionCalls).toBe(1))
 resolveFirstAction(new Response(JSON.stringify({generationId:'generation-1'}),{status:202,headers:{'Content-Type':'application/json'}}))
 await waitFor(()=>expect(FakeEventSource.instances).toHaveLength(1))
 FakeEventSource.instances[0].emit('retrying','ai provider returned invalid output')
 await waitFor(()=>expect(screen.getByRole('status')).toHaveTextContent('повторяю попытку'))
 expect(screen.queryByRole('button',{name:'A'})).not.toBeInTheDocument()
 FakeEventSource.instances[0].emit('failed','ai provider returned invalid output')
 expect(await screen.findByRole('alert')).toHaveTextContent('ai provider returned invalid output')
 const retry=await screen.findByRole('button',{name:'A'})
 fireEvent.click(retry)
 await waitFor(()=>expect(actionCalls).toBe(2))
 vi.unstubAllGlobals();sessionStorage.clear();history.pushState({},'', '/')
})
