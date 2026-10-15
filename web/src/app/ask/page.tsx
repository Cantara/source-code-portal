import { askRoute } from "@/features/assistant/route"
import { page } from "@/lib/page"

const p = page(askRoute)
export default p.Page
export const generateMetadata = p.generateMetadata
