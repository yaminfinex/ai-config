import { Component, type ErrorInfo, type ReactNode } from 'react'

type ErrorBoundaryProps = {
  children: ReactNode
  context?: string
}

type ErrorBoundaryState = {
  error: Error | null
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('Herder interface error', error, info)
  }

  render() {
    if (!this.state.error) return this.props.children
    return <section className={`error-boundary${this.props.context ? ' panel-error-boundary' : ''}`} role="alert">
      {this.props.context && <strong>{this.props.context} failed</strong>}
      <p>{this.state.error.message}</p>
      <button type="button" onClick={() => window.location.reload()}>Reload</button>
    </section>
  }
}
