import { Component, type ErrorInfo, type ReactNode } from 'react'

type ErrorBoundaryProps = {
  children: ReactNode
  context?: string
}

type ErrorBoundaryState = {
  failed: boolean
  error: unknown
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { failed: false, error: null }

  static getDerivedStateFromError(error: unknown): ErrorBoundaryState {
    return { failed: true, error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('Herder interface error', error, info)
  }

  render() {
    if (!this.state.failed) return this.props.children
    return <section className="error-boundary" role="alert">
      <strong>{this.props.context ?? 'Herder interface'} failed</strong>
      {this.state.error instanceof Error && this.state.error.message && <p>{this.state.error.message}</p>}
      <button type="button" onClick={() => window.location.reload()}>Reload</button>
    </section>
  }
}
