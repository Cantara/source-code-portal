"use client"

import { createContext, useContext, type ReactNode } from "react"
import type { Rules } from "./rules"

/**
 * none are no rules: what a page has when the server could not be read, which then says so; it names nothing it cannot know. Its
 * features are on, so the header still leads to the pages that say so.
 */
const none: Rules = {
  windowDays: 0,
  levels: [],
  severities: [],
  activity: [],
  findings: [],
  features: { pulls: true, issues: true, people: false },
}

const Context = createContext<Rules>(none)

/** RulesProvider passes the rules the root layout read from the server to every client component. */
export function RulesProvider({ rules, children }: { rules: Rules | undefined; children: ReactNode }) {
  return <Context value={rules ?? none}>{children}</Context>
}

/** useRules is the server's rules; none when they could not be read (the page then says the API is unreachable). */
export function useRules(): Rules {
  return useContext(Context)
}

/** useFeatures is the parts of the portal switched on (features in scp.yaml): a switched-off one has no link, panel or element. */
export function useFeatures(): Rules["features"] {
  return useContext(Context).features
}

/** Switched renders its children only while feature is switched on. */
export function Switched({ feature, children }: { feature: keyof Rules["features"]; children: ReactNode }) {
  return useFeatures()[feature] ? children : null
}
