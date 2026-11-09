import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, test, vi } from 'vitest'
import { MemorySettings, type MemoryOptions } from '../src/app/MemorySettings'

afterEach(()=>{cleanup();vi.unstubAllGlobals()})
const ready={enabled:true,state:'serving',revision:1001,servingRevision:1001,queued:0,running:0,completed:20,failed:0}
function show(value:MemoryOptions,saved=value){
 const client=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 const onChange=vi.fn()
 render(<QueryClientProvider client={client}><MemorySettings value={value} saved={saved} revision="revision" onChange={onChange}/></QueryClientProvider>)
 return {client,onChange}
}
test('disabled memory cannot be reindexed and controls expose safe defaults',async()=>{
 vi.stubGlobal('fetch',vi.fn(async()=>new Response(JSON.stringify({...ready,enabled:false,state:'disabled'}))))
 const {client,onChange}=show({enabled:false,topK:8,tokenBudget:1024})
 expect(await screen.findByText('Выключена')).toBeInTheDocument()
 expect(screen.getByRole('button',{name:'Переиндексировать память'})).toBeDisabled()
 fireEvent.click(screen.getByRole('checkbox'))
 expect(onChange).toHaveBeenCalledWith({enabled:true,topK:8,tokenBudget:1024})
 client.clear()
})
test('requires saved options before reindexing',async()=>{
 vi.stubGlobal('fetch',vi.fn(async()=>new Response(JSON.stringify(ready))))
 const {client}=show({enabled:true,topK:16,tokenBudget:2048},{enabled:true,topK:8,tokenBudget:1024})
 await screen.findByText(/Готова/)
 expect(screen.getByRole('button',{name:'Переиндексировать память'})).toBeDisabled()
 expect(screen.getByText(/Сначала сохраните настройки/)).toBeInTheDocument()
 client.clear()
})
test('reindex sends a JSON request and building preserves the previous version',async()=>{
 const fetchMock=vi.fn(async(_input:RequestInfo|URL,init?:RequestInit)=>new Response(JSON.stringify(init?.method==='POST'?{...ready,state:'building'}:ready)))
 vi.stubGlobal('fetch',fetchMock)
 const {client}=show({enabled:true,topK:8,tokenBudget:1024})
 await screen.findByText(/Готова/)
 fireEvent.click(screen.getByRole('button',{name:'Переиндексировать память'}))
 await waitFor(()=>expect(fetchMock.mock.calls.some(([url,init])=>String(url).endsWith('/memory/reindex')&&init?.method==='POST'&&init.body==='{}')).toBe(true))
 client.clear()
})
test('shows progress and keeps a build from being started twice',async()=>{
 vi.stubGlobal('fetch',vi.fn(async()=>new Response(JSON.stringify({...ready,state:'building',queued:4,running:1})) ))
 const {client}=show({enabled:true,topK:8,tokenBudget:1024})
 expect(await screen.findByText(/Обновляю память/)).toBeInTheDocument()
 expect(screen.getByText(/используется предыдущая версия/)).toBeInTheDocument()
 expect(screen.getByRole('button',{name:'Переиндексировать память'})).toBeDisabled()
 client.clear()
})
