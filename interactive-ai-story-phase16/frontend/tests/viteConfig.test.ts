import { describe, expect, it } from 'vitest'
import { resolveBackendTarget } from '../src/lib/viteBackendTarget'

describe('Vite backend proxy target', () => {
  it('uses the local backend by default', () => {
    expect(resolveBackendTarget(undefined)).toBe('http://127.0.0.1:8787')
    expect(resolveBackendTarget('   ')).toBe('http://127.0.0.1:8787')
  })

  it('uses the configured Docker or remote backend target', () => {
    expect(resolveBackendTarget(' http://backend:8080 ')).toBe('http://backend:8080')
  })
})
