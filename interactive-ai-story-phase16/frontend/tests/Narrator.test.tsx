import { afterEach, expect, test, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Narrator, type NarrationParagraph, type NarrationStartRequest } from '../src/app/Narrator'

afterEach(()=>{
 cleanup()
 vi.unstubAllGlobals()
 localStorage.clear()
})

function narrationFetch(spoken:string[]){
 return vi.fn(async(input:RequestInfo|URL,init?:RequestInit)=>{
  const url=String(input)
  if(url.endsWith('/api/v1/narration/health'))return new Response(JSON.stringify({status:'ok',device:'test'}),{status:200,headers:{'Content-Type':'application/json'}})
  if(url.endsWith('/api/v1/narration/speech')){
   spoken.push((JSON.parse(String(init?.body)) as {text:string}).text)
   return new Response(new Blob(['test-audio'],{type:'audio/wav'}),{status:200,headers:{'Content-Type':'audio/wav'}})
  }
  return new Response('not found',{status:404})
 })
}

test('waits at the end and continues from a replacement scene without expanding',async()=>{
 const spoken:string[]=[]
 vi.stubGlobal('fetch',narrationFetch(spoken))
 const oldScene:NarrationParagraph[]=[{key:'old-beat:1',label:'Фрагмент 1 · абзац 1',text:'Старый абзац.'}]
 const newScene:NarrationParagraph[]=[{key:'new-beat:1',label:'Фрагмент 1 · абзац 1',text:'Первый абзац новой сцены.'}]
 const start:NarrationStartRequest={key:'old-beat:1',nonce:1}
 const onCurrentChange=vi.fn()
 const view=render(<Narrator timelineId="timeline-1" paragraphs={oldScene} requestedStart={start} onCurrentChange={onCurrentChange}/>)

 await screen.findByText('Читаю сейчас')
 fireEvent.click(screen.getByRole('button',{name:'Свернуть диктор'}))
 fireEvent.ended(view.container.querySelector('audio')!)
 await screen.findByText('Жду продолжение истории')
 expect(view.container.querySelector('.narrator-panel')).toHaveClass('collapsed','enabled')

 view.rerender(<Narrator timelineId="timeline-1" paragraphs={newScene} requestedStart={start} onCurrentChange={onCurrentChange}/>)
 await waitFor(()=>expect(spoken).toContain('Первый абзац новой сцены.'))
 await waitFor(()=>expect(screen.getByText('Читаю сейчас')).toBeInTheDocument())
 expect(view.container.querySelector('.narrator-panel')).toHaveClass('collapsed','enabled')
})

test('an explicit user pause prevents automatic continuation after a scene change',async()=>{
 const spoken:string[]=[]
 vi.stubGlobal('fetch',narrationFetch(spoken))
 const oldScene:NarrationParagraph[]=[{key:'old-beat:1',label:'Фрагмент 1 · абзац 1',text:'Старый абзац.'}]
 const newScene:NarrationParagraph[]=[{key:'new-beat:1',label:'Фрагмент 1 · абзац 1',text:'Не читать автоматически.'}]
 const onCurrentChange=vi.fn()
 const view=render(<Narrator timelineId="timeline-2" paragraphs={oldScene} requestedStart={null} onCurrentChange={onCurrentChange}/>)
 const audio=view.container.querySelector('audio')!
 let paused=true
 Object.defineProperty(audio,'paused',{configurable:true,get:()=>paused})
 Object.defineProperty(audio,'ended',{configurable:true,get:()=>false})
 audio.play=vi.fn(async()=>{paused=false})
 audio.pause=vi.fn(()=>{paused=true})

 const start:NarrationStartRequest={key:'old-beat:1',nonce:1}
 view.rerender(<Narrator timelineId="timeline-2" paragraphs={oldScene} requestedStart={start} onCurrentChange={onCurrentChange}/>)
 await screen.findByText('Читаю сейчас')
 fireEvent.click(screen.getByRole('button',{name:'Ⅱ Пауза'}))
 expect(await screen.findByText('Пауза')).toBeInTheDocument()

 view.rerender(<Narrator timelineId="timeline-2" paragraphs={newScene} requestedStart={start} onCurrentChange={onCurrentChange}/>)
 await new Promise(resolve=>setTimeout(resolve,20))
 expect(spoken).not.toContain('Не читать автоматически.')
 expect(screen.getByText('Пауза')).toBeInTheDocument()
})
