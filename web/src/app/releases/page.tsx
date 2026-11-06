import { releasesRoute } from "@/features/releases/route"
import { page } from "@/lib/page"

const p = page(releasesRoute)
export default p.Page
export const generateMetadata = p.generateMetadata
