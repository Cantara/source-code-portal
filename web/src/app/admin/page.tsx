import { adminRoute } from "@/features/admin/route"
import { page } from "@/lib/page"

const p = page(adminRoute)
export default p.Page
export const generateMetadata = p.generateMetadata
