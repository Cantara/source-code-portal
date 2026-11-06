import { alumniRoute } from "@/features/alumni/route"
import { page } from "@/lib/page"

const p = page(alumniRoute)
export default p.Page
export const generateMetadata = p.generateMetadata
