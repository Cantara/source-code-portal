"use client"

import { createContext, useContext, type ReactNode } from "react"
import type { Rules } from "./rules"

/** none are no rules: what a page has when the server could not be read, which then says so; it names nothing it cannot know. */
const none: Rules = { windowDays: 0, levels: [], severities: [], activity: [], findings: [] }

const Context = createContext<Rules>(none)

/** RulesProvider passes the rules the root layout read from the server to every client component. */
export function RulesProvider({ rules, children }: { rules: Rules | undefined; children: ReactNode }) {
  return <Context value={rules ?? none}>{children}</Context>
}

/** useRules is the server's rules; none when they could not be read (the page then says the API is unreachable). */
export function useRules(): Rules {
  return useContext(Context)
}
