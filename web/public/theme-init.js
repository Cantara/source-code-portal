// Applies the stored theme (light, dark or system) before the first paint, so a dark page never flashes white.
// Loaded as a plain script from index.html; the theme toggle stores its choice under "scp-theme".
;(() => {
  let choice = "system"
  try {
    const stored = localStorage.getItem("scp-theme")
    if (stored === "light" || stored === "dark") choice = stored
  } catch {
    // storage unavailable: follow the system
  }
  const dark = choice === "dark" || (choice === "system" && matchMedia("(prefers-color-scheme: dark)").matches)
  const root = document.documentElement
  root.classList.toggle("dark", dark)
  root.style.colorScheme = dark ? "dark" : "light"
})()
