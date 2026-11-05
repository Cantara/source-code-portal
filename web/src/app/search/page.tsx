import { searchRoute } from "@/features/search/route"
import { page } from "@/lib/page"

const p = page(searchRoute)
export default p.Page
export const generateMetadata = p.generateMetadata
