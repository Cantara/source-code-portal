// Only place allowed to turn a server-sanitised string into DOM. Matches CSP `trusted-types scp-html`.
const policy = typeof window !== "undefined" && window.trustedTypes
  ? window.trustedTypes.createPolicy("scp-html", { createHTML: (s) => s })
  : null

export function SafeHtml({ html, className }: { html: string; className?: string }) {
  return <div className={className} dangerouslySetInnerHTML={{ __html: policy ? policy.createHTML(html) : html }} />
}
