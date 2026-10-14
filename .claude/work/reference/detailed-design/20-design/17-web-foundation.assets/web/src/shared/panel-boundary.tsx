import { QueryErrorResetBoundary } from "@tanstack/react-query"
import { Component, type ErrorInfo, type ReactNode } from "react"
import { ProblemAlert } from "./problem-alert"

type Props = { children: ReactNode; onReset: () => void }
type State = { error: unknown }
class Boundary extends Component<Props, State> {
  override state: State = { error: null }
  static getDerivedStateFromError(error: unknown): State {
    return { error }
  }
  override componentDidCatch(error: unknown, info: ErrorInfo) {
    console.error("panel error", error, info.componentStack)
  }
  override render() {
    if (this.state.error === null) return this.props.children
    return (
      <ProblemAlert
        error={this.state.error}
        onRetry={() => {
          this.props.onReset()
          this.setState({ error: null })
        }}
      />
    )
  }
}

/** Independent panels (latest commits, status of a section) fail alone; Retry resets the query errors inside. */
export function PanelBoundary({ children }: { children: ReactNode }) {
  return <QueryErrorResetBoundary>{({ reset }) => <Boundary onReset={reset}>{children}</Boundary>}</QueryErrorResetBoundary>
}
