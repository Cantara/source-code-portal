// The ONLY module allowed to turn a string into DOM (eslint no-restricted-syntax; CSP trusted-types scp-html).
// The input is server-sanitised SafeHTML (architecture 14.3); this component adds no sanitiser on purpose (no DOMPurify).
const policy =
  typeof window !== "undefined" && window.trustedTypes
    ? window.trustedTypes.createPolicy("scp-html", { createHTML: (s: string) => s })
    : null

export function SafeHtml({ html, className }: { html: string; className?: string | undefined }) {
  return <div className={className} dangerouslySetInnerHTML={{ __html: (policy ? policy.createHTML(html) : html) as string }} />
}
