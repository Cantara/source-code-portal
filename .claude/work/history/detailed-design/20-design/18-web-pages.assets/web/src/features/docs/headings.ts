/**
 * A rendered document normally starts with its own `# Title`. The page already has exactly one `h1` (the document header, `PageHeader` or the
 * repository header) and axe `page-has-heading-one` / "one h1 per page" (4.1.3) must hold, so the HTML's h1s are normalised here (V-27: the first
 * draft said "hide the duplicate h1 by CSS when it equals the title", which CSS cannot decide):
 *   - a LEADING h1 whose text equals the page title is hidden (`hidden`), it would repeat the heading above it;
 *   - every other h1 stays visible but becomes a level-2 heading for assistive technology (`role="heading" aria-level="2"`).
 * Only attributes are set (CSP-safe), and the function is idempotent (it runs again whenever the html or the title changes).
 */
export function normaliseDocH1(root: HTMLElement, pageTitle: string): void {
  const lead = root.firstElementChild
  for (const h of root.querySelectorAll<HTMLElement>("h1")) {
    if (h === lead && (h.textContent ?? "").trim() === pageTitle.trim()) { h.hidden = true; continue }
    h.setAttribute("role", "heading"); h.setAttribute("aria-level", "2")
  }
}
