import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { HeroAssistant } from '../src/app/HeroAssistant'
import { applyDirectorExact, assistDirector } from '../src/lib/api'

vi.mock('../src/lib/api',()=>({assistDirector:vi.fn(),applyDirectorExact:vi.fn()}))
afterEach(cleanup)
beforeEach(()=>{
  vi.clearAllMocks()
  vi.mocked(assistDirector).mockResolvedValue({generationId:'g',summary:'Правка настроения',baseRevision:7,operations:[{type:'update_character_state',before:{mood:'Спокоен'},payload:{characterId:'hero',mood:'Рад',currentGoal:'Найти дом',active:true}}]})
})
function mount() {
  const client = new QueryClient({defaultOptions:{mutations:{retry:false},queries:{retry:false}}})
  const applied=vi.fn()
  const component=(revision:number)=><QueryClientProvider client={client}><HeroAssistant timelineId="t" revision={revision} onApplied={applied}/></QueryClientProvider>
  return {...render(component(7)),component,applied}
}
async function propose() {
  fireEvent.change(screen.getByLabelText('Что изменить у героя?'),{target:{value:'Улучши настроение'}})
  fireEvent.click(screen.getByText('Предложить правки'))
  await screen.findByText('Правка настроения')
}
test('shows before/after and only applies on explicit approval',async()=>{
  const view=mount()
  await propose()
  expect(assistDirector).toHaveBeenCalledWith('t',{section:'hero',instruction:'Улучши настроение'})
  expect(screen.getByText('Было: Спокоен')).toBeInTheDocument()
  expect(screen.getByText('Станет: Рад')).toBeInTheDocument()
  expect(applyDirectorExact).not.toHaveBeenCalled()
  fireEvent.click(screen.getByText('Применить эту правку'))
  await waitFor(()=>expect(view.applied).toHaveBeenCalledOnce())
  expect(applyDirectorExact).toHaveBeenCalledWith('t',7,'update_character_state',expect.objectContaining({currentGoal:'Найти дом'}),undefined)
})
test('blocks a proposal after story revision changes',async()=>{
  const view=mount()
  await propose()
  view.rerender(view.component(8))
  expect(screen.getByText('Применить эту правку')).toBeDisabled()
  expect(applyDirectorExact).not.toHaveBeenCalled()
})
