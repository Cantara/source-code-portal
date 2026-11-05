import { pullsRoute } from "@/features/pulls/route"
import { page } from "@/lib/page"

const p = page(pullsRoute)
export default p.Page
export const generateMetadata = p.generateMetadata
