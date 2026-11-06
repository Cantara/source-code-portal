import { insightsRoute } from "@/features/insights/route"
import { page } from "@/lib/page"

const p = page(insightsRoute)
export default p.Page
export const generateMetadata = p.generateMetadata
