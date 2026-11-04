import type { Schemas } from "@/lib/api/client"

type Level = Schemas["HealthLevel"]

/** levels are the health levels with their colour class and label. */
export const levels: Record<Level, { className: string; label: string }> = {
  good: { className: "lvl-active", label: "Good" },
  watch: { className: "lvl-quiet", label: "Watch" },
  attention: { className: "lvl-danger", label: "Needs attention" },
}
