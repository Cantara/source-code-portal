// FOUNDATION CONTRACT: the one Trusted Types policy of the SPA (CSP: require-trusted-types-for 'script'; trusted-types scp-html).
// `createPolicy("scp-html")` may run ONCE per document (a second call with the same name throws unless the CSP lists 'allow-duplicates'),
// so every sink goes through this module: HTML for SafeHtml, and the script URL of the layout Web Worker (`new Worker(url)` is a
// TrustedScriptURL sink; verified in Chromium 2026-10-14: with a plain string the constructor throws "This document requires
// 'TrustedScriptURL' assignment"). The HTML transform is the identity on purpose (the string is server-sanitised SafeHTML; no DOMPurify, ADR-005).
interface Policy { createHTML(s: string): TrustedHTML; createScriptURL(s: string): TrustedScriptURL }
let policy: Policy | null | undefined

function get(): Policy | null {
  if (policy === undefined) {
    try {
      policy = typeof window !== "undefined" && window.trustedTypes
        ? (window.trustedTypes.createPolicy("scp-html", {
            createHTML: (s) => s,
            // same-origin absolute or root-relative URLs only; the worker bundle is /assets/<name>-<hash>.js
            createScriptURL: (s) => {
              const u = new URL(s, window.location.origin)
              if (u.origin !== window.location.origin || !u.pathname.startsWith("/assets/")) throw new TypeError("script URL not allowed")
              return u.href
            },
          }) as Policy)
        : null
    } catch {
      policy = null // policy name already created (HMR) or not allowed: plain strings work wherever Trusted Types is not enforced
    }
  }
  return policy
}

export function toTrustedHtml(html: string): string | TrustedHTML {
  const p = get()
  return p ? p.createHTML(html) : html
}

/** URL for `new Worker(...)`: a TrustedScriptURL when Trusted Types exists, else the plain href. Throws TypeError for a cross-origin or non-/assets/ URL. */
export function toTrustedScriptUrl(href: string): string | TrustedScriptURL {
  const p = get()
  return p ? p.createScriptURL(href) : href
}
