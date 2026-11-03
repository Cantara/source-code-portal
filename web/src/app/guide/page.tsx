import { guideRoute } from "@/features/guide/route"
import { page } from "@/lib/page"

const p = page(guideRoute)
export default p.Page
export const generateMetadata = p.generateMetadata
