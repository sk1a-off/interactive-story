import { cleanup, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, test, vi } from 'vitest'
import { PerchanceWorkerStatus } from '../src/app/PerchanceWorkerStatus'

afterEach(() => { cleanup(); vi.unstubAllGlobals() })

test.each([
  { connected: false, expected: 'Не в сети' },
  { connected: true, expected: 'Онлайн' },
])('shows $expected from the worker heartbeat', async ({ connected, expected }) => {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ connected }), { status: 200 })))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><PerchanceWorkerStatus /></QueryClientProvider>)
  expect(await screen.findByText(new RegExp(expected))).toBeInTheDocument()
  expect(screen.getByRole('link', { name: /Открыть вкладку воркера/ })).toHaveAttribute('href', 'https://perchance.org/interactive-ai-story-worker')
  if (!connected) expect(screen.getByText(/Откройте вкладку воркера/)).toBeInTheDocument()
  client.clear()
})

test('shows the currently running image format and count', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => new Response(JSON.stringify(String(input).endsWith('/worker-status')
    ? { connected: true, activeGenerationId: 'job-4' }
    : { id: 'job-4', status: 'running', width: 512, height: 768, imageCount: 4 }), { status: 200 })))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><PerchanceWorkerStatus /></QueryClientProvider>)
  expect(await screen.findByText(/Рисует иллюстрацию/)).toBeInTheDocument()
  expect(screen.getByText('512 × 768 · вариантов: 4')).toBeInTheDocument()
  client.clear()
})

test('reports a status failure without claiming the worker is online', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => new Response('unavailable', { status: 503 })))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><PerchanceWorkerStatus /></QueryClientProvider>)
  expect(await screen.findByText(/Статус недоступен/)).toBeInTheDocument()
  client.clear()
})
