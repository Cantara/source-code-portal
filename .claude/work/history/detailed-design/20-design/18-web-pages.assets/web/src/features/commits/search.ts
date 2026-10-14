import { z } from "zod"

/** Search params shared by the three commit-log routes. `q` filters the LOADED rows only (the API has no text filter). */
export const commitsSearch = z.object({
  repo: z.string().regex(/^[A-Za-z0-9._-]{1,100}$/).optional().catch(undefined),
  group: z.string().regex(/^[a-z0-9-]{1,63}$/).optional().catch(undefined),
  q: z.string().max(200).optional().catch(undefined),
})
export type CommitsSearch = z.infer<typeof commitsSearch>
