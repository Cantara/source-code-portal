import { docRoute } from "@/features/docs/route"
import { page } from "@/lib/page"

const p = page(docRoute)
export default p.Page
export const generateMetadata = p.generateMetadata
