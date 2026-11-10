import '../styles/structured-output-settings.css'

type Props = { enabled: boolean; onChange: (enabled: boolean) => void }
export function WriterStreamingSettings({ enabled, onChange }: Props) {
 return <fieldset className="image-output-settings structured-output-settings">
  <legend>Показ продолжения</legend>
  <label className="structured-output-toggle"><input type="checkbox" checked={enabled} onChange={event => onChange(event.target.checked)}/> Показывать текст во время генерации</label>
  <small className="muted">Черновик появляется частями и может измениться после проверки. Если модель не поддерживает такой показ, продолжение появится целиком. Применяется после сохранения настроек.</small>
 </fieldset>
}
