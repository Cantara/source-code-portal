import { t } from "@lingui/core/macro"
import { Monitor, Moon, Sun } from "lucide-react"
import { Button } from "@/components/ui/button"
import { setPreference, useThemePreference, type ThemePreference } from "@/lib/theme"

const NEXT: Record<ThemePreference, ThemePreference> = { system: "light", light: "dark", dark: "system" }

export function ThemeToggle() {
  const pref = useThemePreference()
  const Icon = pref === "dark" ? Moon : pref === "light" ? Sun : Monitor
  const name = pref === "dark" ? t`Dark theme` : pref === "light" ? t`Light theme` : t`System theme`
  return (
    <Button variant="ghost" size="icon" aria-label={t`${name}. Activate to change theme.`} onClick={() => setPreference(NEXT[pref])}>
      <Icon aria-hidden="true" />
    </Button>
  )
}
