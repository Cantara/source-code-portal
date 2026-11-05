import { presentRoute } from "@/features/dashboard/route"
import { page } from "@/lib/page"

const p = page(presentRoute)
export default p.Page
export const generateMetadata = p.generateMetadata
