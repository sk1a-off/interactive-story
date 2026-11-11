import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import { App } from '../src/app/App'

afterEach(() => { cleanup(); vi.unstubAllGlobals(); history.pushState({}, '', '/') })

test('selects browser provider and persists reasoning/retries without streaming or Google keys', async () => {
 const active={id:'rev-old',revision:1,active:true,storyLlm:{kind:'story_llm',provider:'google_gemini',model:'antigravity-preview-05-2026',profile:'google-ai-safe-hybrid'},embedding:{kind:'embedding',provider:'local_sentence_transformers',model:'deepvk/USER2-small'},image:{kind:'image',provider:'manual',model:'manual-v1'},public:{generationProfile:'quality',storyLlmEndpoint:'https://generativelanguage.googleapis.com/v1beta/openai',storyLlmContext:1048576,storyLlmKvDType:'managed',storyLlmSafeHybrid:true,storyLlmStreamWriter:true,storyLlmJsonSchema:false},storyLlmSecretConfigured:true,storyLlmSecretCount:2}
 const submissions:unknown[]=[]
 vi.stubGlobal('fetch',vi.fn(async (input:RequestInfo|URL,init?:RequestInit)=>{
  if(String(input).endsWith('/settings/ai/revisions')&&init?.method==='POST'){submissions.push(JSON.parse(String(init.body)));return new Response(JSON.stringify(active))}
  if(String(input).endsWith('/settings/ai'))return new Response(JSON.stringify({revisions:[active]}))
  return new Response(JSON.stringify({enabled:false,connected:false,state:'disabled'}))
 }))
 history.pushState({},'', '/settings/ai')
 const client=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 render(<BrowserRouter><QueryClientProvider client={client}><App/></QueryClientProvider></BrowserRouter>)
 fireEvent.click(await screen.findByRole('button',{name:/ChatGPT в браузере Временные чаты/}))
 expect(screen.getByRole('combobox',{name:'Сила рассуждения'})).toHaveValue('none')
 fireEvent.change(screen.getByRole('combobox',{name:'Сила рассуждения'}),{target:{value:'high'}})
 fireEvent.change(screen.getByRole('combobox',{name:'Повторы невалидного ответа'}),{target:{value:'2'}})
 expect(screen.queryByRole('switch',{name:/стрим/i})).not.toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:'Сохранить и включить ChatGPT в браузере'}))
 await waitFor(()=>expect(submissions).toHaveLength(1))
 expect(submissions[0]).toMatchObject({config:{storyLlm:{provider:'chatgpt_browser',model:'gpt-6',profile:'chatgpt-browser'},public:{storyLlmEndpoint:'http://127.0.0.1:8791',storyLlmContext:16384,storyLlmBrowserReasoning:'high',storyLlmBrowserRetries:2,storyLlmSafeHybrid:false,storyLlmStreamWriter:false,storyLlmJsonSchema:true}},activate:true})
 expect(submissions[0]).not.toHaveProperty('storyLlmApiKeys')
 client.clear()
})

test('restores saved browser reasoning and a zero retry budget', async()=>{
 const active={id:'rev-browser',revision:2,active:true,storyLlm:{kind:'story_llm',provider:'chatgpt_browser',model:'gpt-6',profile:'chatgpt-browser'},embedding:{kind:'embedding',provider:'local_sentence_transformers',model:'deepvk/USER2-small'},image:{kind:'image',provider:'manual',model:'manual-v1'},public:{generationProfile:'quality',storyLlmEndpoint:'http://127.0.0.1:8791',storyLlmContext:16384,storyLlmKvDType:'managed',storyLlmSafeHybrid:false,storyLlmBrowserReasoning:'medium',storyLlmBrowserRetries:0},storyLlmSecretConfigured:false,storyLlmSecretCount:0}
 vi.stubGlobal('fetch',vi.fn(async(input:RequestInfo|URL)=>new Response(JSON.stringify(String(input).endsWith('/settings/ai')?{revisions:[active]}:{enabled:false,connected:false,state:'disabled'}))))
 history.pushState({},'', '/settings/ai');const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 render(<BrowserRouter><QueryClientProvider client={client}><App/></QueryClientProvider></BrowserRouter>)
 expect(await screen.findByRole('combobox',{name:'Сила рассуждения'})).toHaveValue('medium')
 expect(screen.getByRole('combobox',{name:'Повторы невалидного ответа'})).toHaveValue('0')
 client.clear()
})
