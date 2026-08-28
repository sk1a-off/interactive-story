import { Component, type ErrorInfo, type ReactNode } from 'react'

type Props = { children: ReactNode }
type State = { failed: boolean }

export class ErrorBoundary extends Component<Props, State> {
  state: State = { failed: false }

  static getDerivedStateFromError(): State {
    return { failed: true }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('frontend render failure', { error, componentStack: info.componentStack })
  }

  render() {
    if (this.state.failed) {
      return (
        <main className="shell">
          <section className="card" role="alert">
            <h1>Ошибка интерфейса</h1>
            <p>Перезагрузите страницу. Состояние истории хранится на сервере и не зависит от локального UI.</p>
          </section>
        </main>
      )
    }
    return this.props.children
  }
}
