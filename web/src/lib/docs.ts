/** docAddress is a document's page address under /repos/{owner}/{name}/docs/: its path without the extension, the form rendered links
 * use (a path ending in .md is the raw Markdown). */
export function docAddress(path: string): string {
  return path.replace(/\.[^./]+$/, "")
}

/** withoutLeadingH1 drops a document's leading h1: the page's own h1 already names it, and a page has one h1. */
export function withoutLeadingH1(html: string): string {
  return html.replace(/^\s*<h1\b[^>]*>[\s\S]*?<\/h1>\s*/, "")
}
