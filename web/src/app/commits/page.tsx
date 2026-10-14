import { allCommitsRoute } from "@/features/commits/route"
import { page } from "@/lib/page"

const p = page(allCommitsRoute)
export default p.Page
export const generateMetadata = p.generateMetadata
