import { groupCommitsRoute } from "@/features/commits/route"
import { page } from "@/lib/page"

const p = page(groupCommitsRoute)
export default p.Page
export const generateMetadata = p.generateMetadata
