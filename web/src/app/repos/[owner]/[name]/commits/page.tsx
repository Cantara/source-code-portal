import { repoCommitsRoute } from "@/features/commits/route"
import { page } from "@/lib/page"

const p = page(repoCommitsRoute)
export default p.Page
export const generateMetadata = p.generateMetadata
