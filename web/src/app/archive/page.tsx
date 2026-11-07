import { archiveRoute } from "@/features/archive/route"
import { page } from "@/lib/page"

const p = page(archiveRoute)
export default p.Page
export const generateMetadata = p.generateMetadata
