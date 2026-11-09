import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { JSDOM } from 'jsdom'
import { afterEach, describe, expect, test } from 'vitest'

const workerHTML = readFileSync(resolve(process.cwd(), '../tools/perchance/perchance-runtime-worker.html'), 'utf8')
const workerScript = workerHTML.slice(workerHTML.indexOf('<script>') + '<script>'.length, workerHTML.lastIndexOf('</script>'))

const openWindows: JSDOM[] = []
afterEach(() => {
  while (openWindows.length) openWindows.pop()?.window.close()
})

async function waitForResult(element: Element, accept: (value: { images?: string[]; error?: string }) => boolean) {
  for (let attempt = 0; attempt < 100; attempt++) {
    const raw = element.textContent?.trim()
    if (raw) {
      const parsed = JSON.parse(raw) as { images?: string[]; error?: string }
      if (accept(parsed)) return parsed
    }
    await new Promise(resolve => setTimeout(resolve, 5))
  }
  throw new Error('Perchance runtime did not publish the expected result')
}

describe('Perchance runtime mailbox', () => {
  test.each([
    { count: 1, resolution: '512x768', expectedCount: 1 },
    { count: 4, resolution: '768x512', expectedCount: 4 },
    { count: undefined, resolution: '768x768', expectedCount: 2 },
  ])('generates $expectedCount variants at $resolution', async ({ count, resolution, expectedCount }) => {
    const dom = new JSDOM('<div id="go-perchance-job-mailbox"></div><div id="go-perchance-result-mailbox"></div>', {
      runScripts: 'dangerously', url: 'https://worker.perchance.org/interactive-ai-story-worker',
    })
    openWindows.push(dom)
    const calls: { resolution: string }[] = []
    Object.defineProperty(dom.window, 'generateImage', { value: async (options: { resolution: string }) => {
      calls.push(options)
      return `data:image/png;base64,variant-${calls.length}`
    } })
    dom.window.eval(workerScript)
    dom.window.document.getElementById('go-perchance-job-mailbox')!.textContent = JSON.stringify({ id: 'configured-job', prompt: 'a courier', count, resolution })
    const result = await waitForResult(dom.window.document.getElementById('go-perchance-result-mailbox')!, value => value.images?.length === expectedCount)
    expect(result.images).toHaveLength(expectedCount)
    expect(calls).toHaveLength(expectedCount)
    expect(calls.every(options => options.resolution === resolution)).toBe(true)
  })

  test.each([0, 5, 1.5])('rejects invalid count %s before calling provider', async count => {
    const dom = new JSDOM('<div id="go-perchance-job-mailbox"></div><div id="go-perchance-result-mailbox"></div>', {
      runScripts: 'dangerously', url: 'https://worker.perchance.org/interactive-ai-story-worker',
    })
    openWindows.push(dom)
    let calls = 0
    Object.defineProperty(dom.window, 'generateImage', { value: async () => { calls++; return 'data:image/png;base64,test' } })
    dom.window.eval(workerScript)
    dom.window.document.getElementById('go-perchance-job-mailbox')!.textContent = JSON.stringify({ id: 'invalid-job', prompt: 'a courier', count })
    const result = await waitForResult(dom.window.document.getElementById('go-perchance-result-mailbox')!, value => Boolean(value.error))
    expect(result.error).toContain('integer from 1 to 4')
    expect(calls).toBe(0)
  })

  test('retries provider failures and replays a successful result without regenerating', async () => {
    const dom = new JSDOM('<div id="go-perchance-job-mailbox"></div><div id="go-perchance-result-mailbox"></div>', {
      runScripts: 'dangerously',
      url: 'https://worker.perchance.org/interactive-ai-story-worker',
    })
    openWindows.push(dom)

    let calls = 0
    let fail = true
    Object.defineProperty(dom.window, 'generateImage', {
      configurable: true,
      value: async () => {
        calls++
        if (fail) throw new Error('temporary provider failure')
        return `data:image/png;base64,variant-${calls}`
      },
    })
    dom.window.eval(workerScript)

    const job = dom.window.document.getElementById('go-perchance-job-mailbox')!
    const result = dom.window.document.getElementById('go-perchance-result-mailbox')!
    const payload = { id: 'job-1', prompt: 'a courier beneath a station clock', count: 2, resolution: '768x768' }

    job.textContent = JSON.stringify(payload)
    const failed = await waitForResult(result, value => Boolean(value.error))
    expect(failed.error).toContain('temporary provider failure')

    fail = false
    result.textContent = ''
    job.textContent = JSON.stringify(payload)
    const completed = await waitForResult(result, value => value.images?.length === 2)
    expect(completed.images).toHaveLength(2)
    const callsAfterSuccess = calls

    result.textContent = ''
    job.textContent = JSON.stringify(payload)
    const replayed = await waitForResult(result, value => value.images?.length === 2)
    expect(replayed.images).toEqual(completed.images)
    expect(calls).toBe(callsAfterSuccess)
  })
})
