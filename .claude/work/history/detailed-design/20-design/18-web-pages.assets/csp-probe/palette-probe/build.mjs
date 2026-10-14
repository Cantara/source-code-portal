// usage: node build.mjs <path-to-web>   (needs esbuild; writes app.js next to this file)
import { build } from "esbuild"
import { fileURLToPath } from "node:url"
const here = fileURLToPath(new URL(".", import.meta.url))
const A = process.argv[2]
await build({ entryPoints: [here + "app.ts"], bundle: true, format: "esm", outfile: here + "app.js", alias: { "@": A + "/src" }, platform: "browser", nodePaths: [A + "/node_modules"], logLevel: "error" })
