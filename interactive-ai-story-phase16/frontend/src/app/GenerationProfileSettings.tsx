import type { GenerationProfile } from '../lib/api'

type Props = { value: GenerationProfile; onChange: (value: GenerationProfile) => void }

export function GenerationProfileSettings({ value, onChange }: Props) {
 return <fieldset className="image-output-settings">
  <legend>Профиль генерации</legend>
  <label>Режим продолжения<select value={value} onChange={event => {
   if (event.target.value === 'quality' || event.target.value === 'balance') onChange(event.target.value)
  }}>
   <option value="quality">Качество</option>
   <option value="balance">Баланс</option>
   <option value="speed" disabled>Скорость — пока недоступна</option>
  </select></label>
  <small className="muted">{value === 'balance'
   ? 'Действие, цель и темп планируются вместе. Если общее планирование уже включено, время ожидания останется прежним.'
   : 'Сохраняет текущий порядок генерации и проверок.'} Модели остаются выбранными выше. Профиль применяется к новым ходам после сохранения настроек.</small>
  <small className="muted">Режим «Скорость» появится после проверки сохранений и вариантов действий при отложенных оценках. Пока можно включить показ текста во время генерации.</small>
 </fieldset>
}
