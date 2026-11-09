import '../styles/structured-output-settings.css'

type Props = { enabled: boolean; onChange: (enabled: boolean) => void }

export function StructuredOutputSettings({ enabled, onChange }: Props) {
 return <fieldset className="image-output-settings structured-output-settings">
  <legend>Формат ответов</legend>
  <label className="structured-output-toggle"><input type="checkbox" checked={enabled} onChange={event => onChange(event.target.checked)}/> Стабильный формат ответов</label>
  <small className="muted">Экспериментальная настройка для генерации продолжений. Задаёт структуру ответов модели; содержание по-прежнему проверяется правилами истории. Включается после сохранения настроек.</small>
 </fieldset>
}
