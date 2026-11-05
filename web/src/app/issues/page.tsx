import { issuesRoute } from "@/features/issues/route"
import { page } from "@/lib/page"

const p = page(issuesRoute)
export default p.Page
export const generateMetadata = p.generateMetadata
