export type BrowserReasoning = 'none' | 'medium' | 'high'
type Props = { reasoning: BrowserReasoning; retries: number; onReasoning: (value: BrowserReasoning) => void; onRetries: (value: number) => void }

export function ChatGPTBrowserSettings({ reasoning, retries, onReasoning, onRetries }: Props) {
 return <fieldset className="image-output-settings">
  <legend>ChatGPT в браузере</legend>
  <p className="muted">Для каждого запроса открывается новый временный чат с GPT-6 и прикрепляется полный запрос в UTF-8 TXT. Рабочие окна Chromium должны быть запущены с userscript версии 0.4.0.</p>
  <label>Сила рассуждения<select value={reasoning} onChange={event => onReasoning(event.target.value as BrowserReasoning)}>
   <option value="none">Instant — быстрый ответ</option><option value="medium">Medium — средняя</option><option value="high">High — высокая</option>
  </select></label>
  <small className="muted">Выбранный режим проверяется в рабочем окне перед отправкой. Medium и High могут потребовать больше времени; доступность зависит от аккаунта ChatGPT.</small>
  <label>Повторы невалидного ответа<select value={retries} onChange={event => onRetries(Number(event.target.value))}>
   <option value={0}>Без повторов</option><option value={1}>Один повтор</option><option value={2}>Два повтора</option>
  </select></label>
  <p className="provider-notice">Проверяются JSON, обязательные поля и форма ответа для роли. Пустой или невалидный ответ повторяется в новом временном чате в пределах выбранного лимита. Проверки мира, ресурсов и квестов выполняются перед сохранением истории.</p>
  <small className="muted">Отмена, потеря связи и неопределённый результат отправки останавливают запрос без автоматического повтора. Текст продолжения показывается после проверки ответа.</small>
  <a href="http://127.0.0.1:8791" target="_blank" rel="noreferrer">Открыть локальный мост ↗</a>
 </fieldset>
}
