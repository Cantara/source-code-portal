import { settingsRoute } from "@/features/settings/route"
import { page } from "@/lib/page"

const p = page(settingsRoute)
export default p.Page
export const generateMetadata = p.generateMetadata
