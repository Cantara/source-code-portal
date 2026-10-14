"use client"

import { Component, type ReactNode } from "react"
import { ProblemAlert } from "./problem-alert"

/** PanelBoundary keeps a failing panel from taking the page down: it shows the problem in place of the panel. */
export class PanelBoundary extends Component<{ children: ReactNode }, { error: unknown }> {
  state = { error: undefined as unknown }

  static getDerivedStateFromError(error: unknown) {
    return { error }
  }

  render() {
    return this.state.error !== undefined ? <ProblemAlert error={this.state.error} /> : this.props.children
  }
}
