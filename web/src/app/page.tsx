import { dashboardRoute } from "@/features/dashboard/route"
import { page } from "@/lib/page"

const p = page(dashboardRoute)
export default p.Page
export const generateMetadata = p.generateMetadata
