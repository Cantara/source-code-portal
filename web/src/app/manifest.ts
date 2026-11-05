import type { MetadataRoute } from "next"

/**
 * manifest is the web app manifest of the presentation page (design/present.md): added to a tablet's home screen, it opens /present
 * full screen, in landscape, dark.
 */
export default function manifest(): MetadataRoute.Manifest {
  return {
    name: "Source Code Portal",
    short_name: "SCP",
    description: "The fleet on one screen: products, health and the latest commits.",
    start_url: "/present",
    scope: "/",
    display: "standalone",
    orientation: "landscape",
    background_color: "#0b0f19",
    theme_color: "#0b0f19",
    icons: [{ src: "/favicon.svg", sizes: "any", type: "image/svg+xml" }],
  }
}
