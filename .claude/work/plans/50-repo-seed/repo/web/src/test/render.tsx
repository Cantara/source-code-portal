import { I18nProvider } from "@lingui/react"
import { render, type RenderOptions } from "@testing-library/react"
import type { ReactElement, ReactNode } from "react"
import { i18n } from "@/lib/i18n"

function Wrapper({ children }: { children: ReactNode }) {
  return <I18nProvider i18n={i18n}>{children}</I18nProvider>
}

/** Components that use <Trans> need the provider (a wrapper, so `rerender` keeps it); add routing/query providers per test. */
export function renderI18n(ui: ReactElement, options?: Omit<RenderOptions, "wrapper">) {
  return render(ui, { wrapper: Wrapper, ...options })
}
