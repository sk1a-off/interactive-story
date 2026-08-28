import { afterEach, expect, test, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { SetupAssistant } from '../src/app/SetupAssistant'
import type { SetupComponent } from '../src/lib/api'

afterEach(()=>cleanup())

test('uses a selected location chip to request one addressable delete operation', async () => {
  const components: SetupComponent[] = [{ storyId: 'story-1', key: 'world', revision: 1, source: 'ai', locked: false, status: 'ready', payload: { name: 'City', locations: [{ name: 'Old Port', description: 'Foggy docks' }, { name: 'Tower', description: 'A signal tower' }] } }]
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    void input
    void init
    return new Response(JSON.stringify({ generationId: 'generation-1', summary: 'Удалить только порт', changes: [{ key: 'world', before: components[0].payload, after: { name: 'City', locations: [{ name: 'Tower', description: 'A signal tower' }] }, changedPaths: ['locations'], operations: [{ component: 'world', operation: 'remove_item', path: 'locations', matchField: 'name', matchValue: 'Old Port' }] }] }), { status: 200, headers: { 'Content-Type': 'application/json' } })
  })
  vi.stubGlobal('fetch', fetchMock)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  render(<QueryClientProvider client={client}><SetupAssistant storyId="story-1" components={components} selectedKey="world" onSelectComponent={() => undefined} onDraftsApplied={() => undefined} /></QueryClientProvider>)

  fireEvent.click(screen.getByRole('button', { name: 'Old Port' }))
  fireEvent.click(screen.getByRole('button', { name: 'Удалить' }))

  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
  const body = JSON.parse(String((fetchMock.mock.calls[0][1] as RequestInit).body)) as { action: string; target: Record<string, string>; components: string[] }
  expect(body.action).toBe('remove_item')
  expect(body.components).toEqual(['world'])
  expect(body.target).toEqual({ path: 'locations', matchField: 'name', matchValue: 'Old Port' })
  expect(await screen.findByText('Удалить элемент: Old Port')).toBeInTheDocument()
  vi.unstubAllGlobals()
})

test('shows nested quest and stage controls for the initial quest section', () => {
  const components: SetupComponent[] = [{ storyId: 'story-1', key: 'initial_quests', revision: 1, source: 'ai', locked: false, status: 'ready', payload: { quests: [{ title: 'Stop the storm', stages: [{ kind: 'task', title: 'Reach the lighthouse' }, { kind: 'milestone', title: 'Restart the beacon' }] }, { title: 'Find the crew', stages: [{ kind: 'event', title: 'Decode the signal' }] }] } }]
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><SetupAssistant storyId="story-1" components={components} selectedKey="initial_quests" onSelectComponent={() => undefined} onDraftsApplied={() => undefined} /></QueryClientProvider>)
  expect(screen.getByRole('button', { name: '＋ Квест' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Stop the storm' }))
  expect(screen.getByRole('button', { name: '＋ Этап' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Reach the lighthouse' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Restart the beacon' })).toBeInTheDocument()
})

test('clears the previous section instruction and stale error when switching sections', async () => {
  const components: SetupComponent[] = [
    { storyId: 'story-1', key: 'visual_bible', revision: 1, source: 'ai', locked: false, status: 'ready', payload: { style: 'manga', continuityRules: [] } },
    { storyId: 'story-1', key: 'opening_situation', revision: 1, source: 'ai', locked: false, status: 'ready', payload: { text: 'Opening', choices: ['A', 'B', 'C', 'D'] } },
  ]
  vi.stubGlobal('fetch', vi.fn(async () => new Response('setup assistant returned an invalid proposal', { status: 422 })))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  const view = render(<QueryClientProvider client={client}><SetupAssistant storyId="story-1" components={components} selectedKey="visual_bible" onSelectComponent={() => undefined} onDraftsApplied={() => undefined} /></QueryClientProvider>)
  const instruction = screen.getByLabelText('Своя просьба для этого раздела')
  fireEvent.change(instruction, { target: { value: 'Измени визуальный стиль' } })
  fireEvent.click(screen.getByRole('button', { name: 'Предложить изменения раздела' }))
  expect(await screen.findByRole('alert')).toBeInTheDocument()

  view.rerender(<QueryClientProvider client={client}><SetupAssistant storyId="story-1" components={components} selectedKey="opening_situation" onSelectComponent={() => undefined} onDraftsApplied={() => undefined} /></QueryClientProvider>)
  await waitFor(() => expect(screen.getByLabelText('Своя просьба для этого раздела')).toHaveValue(''))
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  vi.unstubAllGlobals()
})
