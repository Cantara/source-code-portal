// The ONLY place in the SPA that inserts HTML (ESLint no-restricted-syntax forbids dangerouslySetInnerHTML / innerHTML elsewhere).
// The string is server-sanitised SafeHTML (ADR-005); the Trusted Types policy lives in lib/trusted-types.ts.
import { forwardRef, useMemo } from "react"
import { toTrustedHtml } from "@/lib/trusted-types"

export interface SafeHtmlProps { readonly html: string; readonly className?: string; readonly lang?: string }

export const SafeHtml = forwardRef<HTMLDivElement, SafeHtmlProps>(function SafeHtml({ html, className, lang }, ref) {
  // V-02: `policy.createHTML()` returns a NEW TrustedHTML object on every call, and React re-assigns `innerHTML` whenever the `__html` value
  // is not `===` the previous one. Without this memo every re-render of a parent (a refetch, a scroll-spy state change) would rebuild the
  // whole document DOM, drop the Copy buttons added by enhanceCodeBlocks, lose focus and the scroll position of code blocks. Keyed on the string.
  const inner = useMemo(() => ({ __html: toTrustedHtml(html) as string }), [html])
  return <div ref={ref} className={className} {...(lang ? { lang } : {})} dangerouslySetInnerHTML={inner} />
})
