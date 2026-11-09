const defaultBackendTarget = 'http://127.0.0.1:8787'

export function resolveBackendTarget(value?: string) {
  return value?.trim() || defaultBackendTarget
}
