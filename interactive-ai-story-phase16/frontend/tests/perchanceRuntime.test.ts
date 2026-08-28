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
