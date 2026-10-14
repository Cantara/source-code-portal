import { z } from "zod"
/** /dashboard?q= : filters the group cards client-side (the whole list is one page, at most a few dozen groups). */
export const dashboardSearch = z.object({ q: z.string().max(200).optional().catch(undefined) })
export type DashboardSearch = z.infer<typeof dashboardSearch>
