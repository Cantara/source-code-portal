import { repoRoute } from "@/features/repos/route"
import { page } from "@/lib/page"
import { panels } from "./panels"

const p = page(repoRoute(panels))
export default p.Page
export const generateMetadata = p.generateMetadata
