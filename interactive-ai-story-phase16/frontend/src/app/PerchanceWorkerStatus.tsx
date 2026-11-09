import { useQuery } from '@tanstack/react-query'
import { getImageGeneration, getImageWorkerStatus } from '../lib/api'
import '../styles/illustrations.css'

export function PerchanceWorkerStatus() {
  const status = useQuery({ queryKey: ['image-worker-status'], queryFn: getImageWorkerStatus, refetchInterval: 10000, retry: false })
  const activeId = status.data?.connected && !status.isError ? status.data.activeGenerationId : undefined
  const job = useQuery({ queryKey: ['image-worker-job', activeId], queryFn: () => getImageGeneration(activeId!), enabled: Boolean(activeId), refetchInterval: activeId ? 10000 : false, retry: false })
  const online = Boolean(status.data?.connected) && !status.isError
  const drawing = online && activeId && job.data?.status === 'running'
  const label = status.isPending ? 'Проверяю связь…' : status.isError ? 'Статус недоступен' : !online ? 'Не в сети' : drawing ? 'Рисует иллюстрацию' : activeId && job.isPending ? 'Уточняю задание…' : 'Онлайн'
  return <aside className={`perchance-worker-status${online ? ' online' : ''}`} aria-label="Воркер Perchance">
    <span><strong>Perchance</strong> · {label}</span>
    {drawing && job.data && <small>{job.data.width} × {job.data.height} · вариантов: {job.data.imageCount}</small>}
    {!online && !status.isPending && <small>Откройте вкладку воркера и оставьте её открытой для создания иллюстраций.</small>}
    <a href="https://perchance.org/interactive-ai-story-worker" target="_blank" rel="noreferrer">Открыть вкладку воркера ↗</a>
  </aside>
}
