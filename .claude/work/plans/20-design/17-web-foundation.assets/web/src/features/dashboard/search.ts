import * as z from "zod/mini"

export const dashboardSearch = z.object({ q: z.optional(z.string().check(z.maxLength(200))) })
export type DashboardSearch = z.infer<typeof dashboardSearch>
