import type { MetadataRoute } from "next"
import { look, productName } from "@/lib/look"

/**
 * manifest is the web app manifest of the presentation page (design/present.md): added to a tablet's home screen, it opens /present
 * full screen, in landscape, dark.
 */
// Read at the request: the deployment's name and icon come from the environment of the running server, not of the build.
export const dynamic = "force-dynamic"

export default function manifest(): MetadataRoute.Manifest {
  const { name, favicon } = look()
  return {
    name,
    short_name: name === productName ? "SCP" : name.slice(0, 12),
    description: "The fleet on one screen: products, health and the latest commits.",
    start_url: "/present",
    scope: "/",
    display: "standalone",
    orientation: "landscape",
    background_color: "#0b0f19",
    theme_color: "#0b0f19",
    icons: [{ src: favicon ?? "/favicon.svg", sizes: "any", type: "image/svg+xml" }],
  }
}
