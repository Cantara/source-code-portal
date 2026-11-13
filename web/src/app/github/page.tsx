import { githubRoute } from "@/features/github/route"
import { page } from "@/lib/page"

const p = page(githubRoute)
export default p.Page
export const generateMetadata = p.generateMetadata
