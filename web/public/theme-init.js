// Applies the stored theme (light, dark or system) before the first paint, so a dark page never flashes white.
// Loaded as a plain script from index.html; the theme toggle stores its choice under "scp-theme". A screen on a wall (/present told
// so by ?profile=wall or the cookie scp-present-wall) is dark unless the reader chose (design/present.md); every other page, the
// presentation page on a tablet or a desk too, follows the system. src/lib/theme.ts paints the same.
;(() => {
  let choice = "system"
  try {
    const stored = localStorage.getItem("scp-theme")
    if (stored === "light" || stored === "dark") choice = stored
  } catch {
    // storage unavailable: follow the system
  }
  const ask = new URLSearchParams(location.search).get("profile")
  const wall =
    location.pathname === "/present" &&
    (ask === "wall" || (ask !== "auto" && /(?:^|;\s*)scp-present-wall=1(?:;|$)/.test(document.cookie)))
  const dark = choice === "dark" || (choice === "system" && (wall || matchMedia("(prefers-color-scheme: dark)").matches))
  const root = document.documentElement
  root.classList.toggle("dark", dark)
  root.style.colorScheme = dark ? "dark" : "light"
})()
