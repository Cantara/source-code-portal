import { signInRoute } from "@/features/signin/route"
import { page } from "@/lib/page"

const p = page(signInRoute)
export default p.Page
export const generateMetadata = p.generateMetadata
