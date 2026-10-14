import { groupRoute } from "@/features/groups/route"
import { page } from "@/lib/page"

const p = page(groupRoute)
export default p.Page
export const generateMetadata = p.generateMetadata
