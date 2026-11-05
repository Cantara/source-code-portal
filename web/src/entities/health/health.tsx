import type { Schemas } from "@/lib/api/client"

type Level = Schemas["HealthLevel"]

/** levels are the health levels' colours; their names and the scores they start at come from the server's rules (lib/rules.ts). */
export const levels: Record<Level, { className: string }> = {
  good: { className: "lvl-active" },
  watch: { className: "lvl-quiet" },
  attention: { className: "lvl-danger" },
}
