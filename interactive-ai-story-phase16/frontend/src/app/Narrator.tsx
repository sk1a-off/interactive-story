import { useCallback, useEffect, useRef, useState } from 'react'
import { getNarrationHealth, synthesizeNarration } from '../lib/api'

export type NarrationParagraph={key:string;label:string;text:string}
export type NarrationStartRequest={key:string;nonce:number}

const BUFFER_TARGET=3

function formatTime(value:number){
 if(!Number.isFinite(value)||value<0)return '0:00'
 const seconds=Math.floor(value)
 return `${Math.floor(seconds/60)}:${String(seconds%60).padStart(2,'0')}`
}

export function SpeakerIcon(){
 return <svg className="paragraph-action-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 4a4 4 0 0 0-4 4v4a4 4 0 0 0 8 0V8a4 4 0 0 0-4-4Z"/><path d="M5 11v1a7 7 0 0 0 14 0v-1M12 19v3M8.5 22h7"/></svg>
}

export function Narrator({timelineId,paragraphs,requestedStart,onCurrentChange}:{timelineId:string;paragraphs:NarrationParagraph[];requestedStart:NarrationStartRequest|null;onCurrentChange:(key:string|null)=>void}){
 const [expanded,setExpanded]=useState(()=>{try{return localStorage.getItem(`reader-narrator-open:${timelineId}`)!=='closed'}catch{return true}})
 const [enabled,setEnabled]=useState(false)
 const [health,setHealth]=useState<'checking'|'online'|'offline'>('checking')
 const [healthDetail,setHealthDetail]=useState('Проверяю OmniVoice…')
 const [selectedKey,setSelectedKey]=useState(paragraphs[0]?.key??'')
 const [currentKey,setCurrentKey]=useState<string|null>(null)
 const [currentLabel,setCurrentLabel]=useState('')
 const [state,setState]=useState<'idle'|'buffering'|'playing'|'paused'|'waiting'|'finished'|'error'>('idle')
 const [error,setError]=useState('')
 const [buffered,setBuffered]=useState(0)
 const [elapsed,setElapsed]=useState(0)
 const [duration,setDuration]=useState(0)
 const [speed,setSpeed]=useState(1)
 const audioRef=useRef<HTMLAudioElement|null>(null)
 const paragraphsRef=useRef(paragraphs);paragraphsRef.current=paragraphs
 const currentKeyRef=useRef<string|null>(null)
 const cacheRef=useRef(new Map<string,string>())
 const pendingRef=useRef(new Map<string,Promise<string>>())
 const controllersRef=useRef(new Set<AbortController>())
 const epochRef=useRef(0)
 const mountedRef=useRef(true)
 const speedRef=useRef(speed);speedRef.current=speed
 const paragraphSignature=paragraphs.map(item=>item.key).join('|')

 const checkHealth=useCallback(async()=>{
  setHealth('checking');setHealthDetail('Проверяю OmniVoice…')
  try{const value=await getNarrationHealth();if(value.status!=='ok')throw new Error(`status: ${value.status}`);setHealth('online');setHealthDetail(`OmniVoice · ${value.device||'готов'}`)}
  catch(err){setHealth('offline');setHealthDetail(err instanceof Error?err.message:'OmniVoice недоступен')}
 },[])

 useEffect(()=>{void checkHealth()},[checkHealth])
 useEffect(()=>{try{localStorage.setItem(`reader-narrator-open:${timelineId}`,expanded?'open':'closed')}catch{/* Optional preference. */}},[expanded,timelineId])
 useEffect(()=>{
  if(selectedKey&&paragraphs.some(item=>item.key===selectedKey))return
  setSelectedKey(paragraphs[0]?.key??'')
 },[paragraphSignature,paragraphs,selectedKey])
 useEffect(()=>{onCurrentChange(enabled&&state!=='waiting'&&currentKey&&paragraphs.some(item=>item.key===currentKey)?currentKey:null)},[currentKey,enabled,onCurrentChange,paragraphSignature,paragraphs,state])
 useEffect(()=>{
 // React StrictMode intentionally mounts, cleans up and mounts again in
 // development. Restore the guard on every setup, not only its initial value.
 mountedRef.current=true
  const controllers=controllersRef.current
  const cache=cacheRef.current
  return()=>{
   mountedRef.current=false
   controllers.forEach(controller=>controller.abort())
   cache.forEach(url=>URL.revokeObjectURL(url))
  }
 },[])

 const updateBuffered=useCallback((fromIndex:number)=>{
  const list=paragraphsRef.current
  let count=0
  for(let index=fromIndex;index<Math.min(list.length,fromIndex+BUFFER_TARGET);index++)if(cacheRef.current.has(list[index].key))count++
  if(mountedRef.current)setBuffered(count)
 },[])

 const ensureAudio=useCallback(async(index:number)=>{
  const paragraph=paragraphsRef.current[index]
  if(!paragraph)throw new Error('Абзац для чтения больше недоступен.')
  const cached=cacheRef.current.get(paragraph.key)
  if(cached)return cached
  const pending=pendingRef.current.get(paragraph.key)
  if(pending)return pending
  const controller=new AbortController();controllersRef.current.add(controller)
  const promise=synthesizeNarration(paragraph.text,controller.signal).then(blob=>{
   if(!blob.size)throw new Error('OmniVoice вернул пустой аудиофайл.')
   const url=URL.createObjectURL(blob)
   if(!mountedRef.current){URL.revokeObjectURL(url);throw new Error('Чтение остановлено.')}
   cacheRef.current.set(paragraph.key,url)
   return url
  }).finally(()=>{controllersRef.current.delete(controller);pendingRef.current.delete(paragraph.key)})
  pendingRef.current.set(paragraph.key,promise)
  return promise
 },[])

 const fillBuffer=useCallback(async(fromIndex:number,epoch:number)=>{
  const list=paragraphsRef.current
  for(let index=fromIndex;index<Math.min(list.length,fromIndex+BUFFER_TARGET);index++){
   if(epochRef.current!==epoch)return
   try{await ensureAudio(index)}catch{if(epochRef.current===epoch)setError('Не удалось заранее подготовить один из абзацев. Повторю при переходе.');return}
   updateBuffered(fromIndex)
  }
 },[ensureAudio,updateBuffered])

 const playIndex=useCallback(async(index:number,{reveal=true}:{reveal?:boolean}={})=>{
  const list=paragraphsRef.current
  if(index<0||index>=list.length)return
  const paragraph=list[index]
  const epoch=++epochRef.current
  currentKeyRef.current=paragraph.key
  setEnabled(true);if(reveal)setExpanded(true);setSelectedKey(paragraph.key);setCurrentKey(paragraph.key);setCurrentLabel(paragraph.label);setState('buffering');setError('');setElapsed(0);setDuration(0);updateBuffered(index)
  const audio=audioRef.current
  if(audio){audio.pause();audio.removeAttribute('src');audio.load()}
  try{
   const url=await ensureAudio(index)
   if(epochRef.current!==epoch||!audioRef.current)return
   audioRef.current.src=url;audioRef.current.playbackRate=speedRef.current;audioRef.current.load()
   await audioRef.current.play()
   if(epochRef.current!==epoch)return
   setState('playing');updateBuffered(index);void fillBuffer(index,epoch)
  }catch(err){
   if(epochRef.current!==epoch)return
   setState('error');setError(err instanceof Error?err.message:'Не удалось начать чтение. Нажмите кнопку ещё раз.')
  }
 },[ensureAudio,fillBuffer,updateBuffered])

 const continueAfterCurrent=useCallback(()=>{
  const list=paragraphsRef.current
  const completedIndex=currentKeyRef.current?list.findIndex(item=>item.key===currentKeyRef.current):-1
  // A new scene or chapter replaces the Reader list, so the old key vanishes
  // and the first paragraph becomes the correct continuation target.
  const nextIndex=completedIndex>=0?completedIndex+1:0
  if(nextIndex<list.length){void playIndex(nextIndex,{reveal:false});return}
  // Keep continuous reading armed while the story worker creates the next beat.
  // Only an explicit pause or power-off cancels this wait.
  setState('waiting');setBuffered(0)
 },[playIndex])

 useEffect(()=>{
  if(!enabled||state!=='waiting')return
  const list=paragraphsRef.current
  const completedIndex=currentKeyRef.current?list.findIndex(item=>item.key===currentKeyRef.current):-1
  const nextIndex=completedIndex>=0?completedIndex+1:0
  if(nextIndex<list.length)void playIndex(nextIndex,{reveal:false})
 },[enabled,paragraphSignature,playIndex,state])

 useEffect(()=>{
  if(!enabled||state!=='playing')return
  const list=paragraphsRef.current
  const activeIndex=currentKeyRef.current?list.findIndex(item=>item.key===currentKeyRef.current):-1
  const nextIndex=activeIndex>=0?activeIndex+1:0
  // Preload a newly committed beat/scene even if the previous audio is still
  // playing, reducing the pause at narrative boundaries.
  if(nextIndex<list.length)void fillBuffer(nextIndex,epochRef.current)
 },[enabled,fillBuffer,paragraphSignature,state])

 useEffect(()=>{
  if(!requestedStart)return
  const index=paragraphsRef.current.findIndex(item=>item.key===requestedStart.key)
  if(index>=0)void playIndex(index)
 },[requestedStart,playIndex])

 function toggleEnabled(){
  if(enabled){epochRef.current++;audioRef.current?.pause();setEnabled(false);setState('idle');setBuffered(0);onCurrentChange(null);return}
  setEnabled(true);setExpanded(true);if(health==='offline')void checkHealth()
 }
 function togglePlayback(){
  const audio=audioRef.current
  if(state==='waiting'){setState('paused');return}
  if(state==='buffering'){epochRef.current++;audio?.pause();setState('paused');return}
  if(!audio||!currentKey||!audio.src){const index=Math.max(0,paragraphs.findIndex(item=>item.key===selectedKey));void playIndex(index);return}
  if(audio.paused){
   if(audio.ended){continueAfterCurrent();return}
   audio.playbackRate=speedRef.current;void audio.play().then(()=>setState('playing')).catch(err=>{setState('error');setError(err instanceof Error?err.message:'Не удалось продолжить чтение.')})
  }else{audio.pause();setState('paused')}
 }
 const currentIndexValue=currentKey?paragraphs.findIndex(item=>item.key===currentKey):-1
 const currentIndex=currentIndexValue>=0?currentIndexValue:null
 const current=currentIndex===null?null:paragraphs[currentIndex]
 const status=health==='offline'?'Сервис недоступен':state==='buffering'?'Подготавливаю аудио':state==='playing'?'Читаю сейчас':state==='paused'?'Пауза':state==='waiting'?'Жду продолжение истории':state==='finished'?'Чтение завершено':state==='error'?'Ошибка озвучивания':'Готов к чтению'

 return <aside className={`narrator-panel ${expanded?'expanded':'collapsed'} ${enabled?'enabled':'disabled'}`} aria-label="Диктор">
  <header><div><p className="eyebrow">Озвучивание</p><h2>Диктор</h2></div><div className="narrator-head-actions"><button className="narrator-collapse" type="button" onClick={()=>setExpanded(value=>!value)} aria-label={expanded?'Свернуть диктор':'Развернуть диктор'}>{expanded?'⌄':'⌃'}</button><button className="narrator-power" type="button" aria-pressed={enabled} onClick={toggleEnabled}><span/>{enabled?'Включён':'Выключен'}</button></div></header>
  <div className="narrator-body" aria-hidden={!expanded}>
   <div className={`narrator-service ${health}`}><span/>{healthDetail}{health==='offline'&&<button type="button" onClick={()=>void checkHealth()}>Проверить</button>}</div>
   <div className="narrator-now"><small>{current?'Сейчас читается':state==='waiting'?'Последний прочитанный':'Точка чтения'}</small><strong>{current?.label||currentLabel||paragraphs.find(item=>item.key===selectedKey)?.label||'Нет текста'}</strong></div>
   <audio ref={audioRef} preload="auto" onPlay={()=>setState('playing')} onLoadedMetadata={event=>setDuration(event.currentTarget.duration)} onTimeUpdate={event=>setElapsed(event.currentTarget.currentTime)} onEnded={continueAfterCurrent}/>
   <div className="narrator-controls"><button type="button" disabled={!enabled||currentIndex===null||currentIndex<=0} onClick={()=>void playIndex((currentIndex??0)-1)} aria-label="Предыдущий абзац">‹</button><button className="narrator-play" type="button" disabled={!enabled||health!=='online'||paragraphs.length===0} onClick={togglePlayback}>{state==='playing'||state==='waiting'?'Ⅱ Пауза':state==='buffering'?'Подготавливаю…':state==='idle'?'▶ Начать чтение':'▶ Продолжить чтение'}</button><button type="button" disabled={!enabled||currentIndex===null||currentIndex>=paragraphs.length-1} onClick={()=>void playIndex((currentIndex??-1)+1)} aria-label="Следующий абзац">›</button></div>
   <div className="narrator-progress"><span>{formatTime(elapsed)}</span><input aria-label="Позиция воспроизведения" type="range" min="0" max={duration||0} step="0.1" value={Math.min(elapsed,duration||0)} disabled={!duration} onChange={event=>{if(audioRef.current){audioRef.current.currentTime=Number(event.target.value);setElapsed(Number(event.target.value))}}}/><span>{formatTime(duration)}</span></div>
   <div className="narrator-buffer"><span>Буфер</span><div>{Array.from({length:BUFFER_TARGET},(_,index)=><i className={index<buffered?'ready':''} key={index}/>)}</div><b>{buffered}/{BUFFER_TARGET}</b></div>
   <div className="narrator-options"><label>Начать с<select value={selectedKey} onChange={event=>setSelectedKey(event.target.value)}>{paragraphs.map(item=><option value={item.key} key={item.key}>{item.label}</option>)}</select></label><label>Темп<select value={speed} onChange={event=>{const value=Number(event.target.value);setSpeed(value);speedRef.current=value;if(audioRef.current)audioRef.current.playbackRate=value}}><option value={0.85}>0.85×</option><option value={1}>1×</option><option value={1.15}>1.15×</option><option value={1.3}>1.3×</option></select></label></div>
   <p className={`narrator-status ${state}`} aria-live="polite">{status}</p>
   {error&&<p className="error narrator-error" role="alert">{error}</p>}
  </div>
 </aside>
}
