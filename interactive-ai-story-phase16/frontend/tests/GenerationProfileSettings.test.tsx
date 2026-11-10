import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import { App } from '../src/app/App'
import { GenerationProfileSettings } from '../src/app/GenerationProfileSettings'
import type { GenerationProfile } from '../src/lib/api'

afterEach(() => { cleanup(); vi.unstubAllGlobals(); history.pushState({}, '', '/') })

test('offers quality and balance while speed remains unavailable', () => {
 const onChange = vi.fn()
 render(<GenerationProfileSettings value="quality" onChange={onChange}/>)
 const control = screen.getByRole('combobox', { name: 'Режим продолжения' })
 expect(control).toHaveValue('quality')
 expect(screen.getByRole('option', { name: 'Скорость — пока недоступна' })).toBeDisabled()
 fireEvent.change(control, { target: { value: 'speed' } })
 expect(onChange).not.toHaveBeenCalled()
 fireEvent.change(control, { target: { value: 'balance' } })
 expect(onChange).toHaveBeenCalledWith('balance')
})

test.each<GenerationProfile | undefined>([undefined, 'quality', 'balance'])('restores and saves the generation profile (%s)', async profile => {
 const active = { id:'revision-1', revision:1, active:true, storyLlm:{kind:'story_llm',provider:'openai_compatible',model:'local-model',profile:'local'}, embedding:{kind:'embedding',provider:'local_sentence_transformers',model:'deepvk/USER2-small'}, image:{kind:'image',provider:'perchance_browser',model:'digital-painting'}, public:{generationProfile:profile,storyLlmEndpoint:'http://127.0.0.1:8081',storyLlmContext:16384,storyLlmKvDType:'q8_0',storyLlmSafeHybrid:false,storyLlmStreamWriter:true,storyLlmJsonSchema:true},storyLlmSecretConfigured:false,storyLlmSecretCount:0 }
 const submissions: unknown[] = []
 vi.stubGlobal('fetch', vi.fn(async (input:RequestInfo|URL, init?:RequestInit) => {
  if (String(input).endsWith('/settings/ai/revisions') && init?.method==='POST') { submissions.push(JSON.parse(String(init.body))); return new Response(JSON.stringify(active)) }
  if (String(input).endsWith('/settings/ai')) return new Response(JSON.stringify({revisions:[active]}))
  return new Response(JSON.stringify({enabled:false,connected:false,state:'disabled'}))
 }))
 history.pushState({},'', '/settings/ai')
 const client=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 render(<BrowserRouter><QueryClientProvider client={client}><App/></QueryClientProvider></BrowserRouter>)
 const control=await screen.findByRole('combobox',{name:'Режим продолжения'})
 await waitFor(()=>expect(control).toHaveValue(profile??'quality'))
 const next=profile==='balance'?'quality':'balance'
 fireEvent.change(control,{target:{value:next}})
 fireEvent.click(screen.getByRole('button',{name:'Сохранить и включить локальную модель'}))
 await waitFor(()=>expect(submissions).toHaveLength(1))
 expect(submissions[0]).toMatchObject({config:{storyLlm:{model:'local-model',provider:'openai_compatible',profile:'local'},public:{generationProfile:next,storyLlmSafeHybrid:false,storyLlmStreamWriter:true,storyLlmJsonSchema:true}},activate:true})
 client.clear()
})
