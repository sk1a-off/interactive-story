import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import { App } from '../src/app/App'
import { WriterStreamingSettings } from '../src/app/WriterStreamingSettings'

afterEach(() => { cleanup(); vi.unstubAllGlobals(); history.pushState({}, '', '/') })

test('keeps the experimental mode off until explicitly selected', () => {
 const onChange = vi.fn()
 const { rerender } = render(<WriterStreamingSettings enabled={false} onChange={onChange}/>)
 const control = screen.getByRole('checkbox', { name: 'Показывать текст во время генерации' })
 expect(control).not.toBeChecked()
 fireEvent.click(control)
 expect(onChange).toHaveBeenCalledWith(true)
 rerender(<WriterStreamingSettings enabled onChange={onChange}/>)
 expect(control).toBeChecked()
 fireEvent.click(control)
 expect(onChange).toHaveBeenLastCalledWith(false)
})

test.each([false, true])('AI settings restore and save the schema flag (%s)', async enabled => {
 const active = { id:'revision-1', revision:1, active:true, storyLlm:{kind:'story_llm',provider:'openai_compatible',model:'local-model',profile:'local'}, embedding:{kind:'embedding',provider:'local_sentence_transformers',model:'deepvk/USER2-small'}, image:{kind:'image',provider:'perchance_browser',model:'digital-painting'}, public:{storyLlmEndpoint:'http://127.0.0.1:8081',storyLlmContext:16384,storyLlmKvDType:'q8_0',storyLlmSafeHybrid:false,storyLlmStreamWriter:enabled},storyLlmSecretConfigured:false,storyLlmSecretCount:0 }
 const submissions: unknown[] = []
 vi.stubGlobal('fetch', vi.fn(async (input:RequestInfo|URL, init?:RequestInit) => {
  if (String(input).endsWith('/settings/ai/revisions') && init?.method==='POST') { submissions.push(JSON.parse(String(init.body))); return new Response(JSON.stringify(active)) }
  if (String(input).endsWith('/settings/ai')) return new Response(JSON.stringify({revisions:[active]}))
  return new Response(JSON.stringify({enabled:false,connected:false,state:'disabled'}))
 }))
 history.pushState({},'', '/settings/ai')
 const client=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 render(<BrowserRouter><QueryClientProvider client={client}><App/></QueryClientProvider></BrowserRouter>)
 const control=await screen.findByRole('checkbox',{name:'Показывать текст во время генерации'})
 await waitFor(()=>expect(control).toHaveProperty('checked',enabled))
 fireEvent.click(control)
 fireEvent.click(screen.getByRole('button',{name:'Сохранить и включить локальную модель'}))
 await waitFor(()=>expect(submissions).toHaveLength(1))
 expect(submissions[0]).toMatchObject({config:{public:{storyLlmStreamWriter:!enabled}},activate:true})
 client.clear()
})
