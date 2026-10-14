"use client"

import { Component, type ReactNode } from "react"
import { Button } from "@/components/ui/button"
import { ProblemAlert } from "./problem-alert"

/**
 * PanelBoundary keeps a failing panel from taking the page down: it shows the problem in place of the panel, with Retry to render
 * the panel again.
 */
export class PanelBoundary extends Component<{ children: ReactNode }, { error: unknown }> {
  state = { error: undefined as unknown }

  static getDerivedStateFromError(error: unknown) {
    return { error }
  }

  render() {
    if (this.state.error === undefined) return this.props.children
    return (
      <div>
        <ProblemAlert error={this.state.error} />
        <Button variant="outline" className="mt-2" onClick={() => this.setState({ error: undefined })}>
          Retry
        </Button>
      </div>
    )
  }
}
