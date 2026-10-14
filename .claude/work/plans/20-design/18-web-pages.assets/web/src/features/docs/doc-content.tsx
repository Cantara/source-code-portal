import { useEffect, useMemo, useRef } from "react"
import { useRouter } from "@tanstack/react-router"
import { SafeHtml } from "@/shared/safe-html"
import type { components } from "@/lib/api/schema"
import { normaliseDocH1 } from "./headings"
import { buildLinkIndex, decideClick } from "./links"

type Doc = components["schemas"]["Doc"]

/** Sanitised HTML + delegated link handling + progressive enhancement (copy buttons, mermaid label). No Markdown is parsed here. */
export function DocContent({ doc, pageTitle }: { readonly doc: Pick<Doc, "html" | "links" | "id" | "renderedBy">; readonly pageTitle: string }) {
  const router = useRouter()
  const ref = useRef<HTMLDivElement>(null)
  const index = useMemo(() => buildLinkIndex(doc.links), [doc.links])

  useEffect(() => {
    const root = ref.current
    if (!root) return
    const added: HTMLElement[] = []
    normaliseDocH1(root, pageTitle)
    for (const pre of root.querySelectorAll<HTMLElement>("pre")) {
      const code = pre.querySelector("code")
      if (!code) continue
      pre.tabIndex = 0 // a scrollable region must be keyboard reachable (axe: scrollable-region-focusable)
      if (code.className.includes("language-mermaid")) pre.dataset["label"] = "Diagram source (Mermaid)"
      const b = document.createElement("button")
      b.type = "button"; b.className = "copy-code"; b.textContent = "Copy"; b.setAttribute("aria-label", "Copy code to clipboard")
      b.addEventListener("click", () => { void navigator.clipboard.writeText(code.textContent ?? "") })
      pre.append(b); added.push(b)
    }
    return () => { for (const b of added) b.remove() } // the html is re-set by React when it changes; never leave stale buttons
  }, [doc.html, pageTitle])

  const onClick = (e: React.MouseEvent<HTMLDivElement>) => {
    const a = (e.target as HTMLElement).closest("a")
    if (!a || !e.currentTarget.contains(a)) return
    const d = decideClick(a.getAttribute("href"), { button: e.button, ctrl: e.ctrlKey, meta: e.metaKey, shift: e.shiftKey, alt: e.altKey, target: a.getAttribute("target"), download: a.hasAttribute("download") }, index)
    if (d.action === "navigate") { e.preventDefault(); void router.navigate({ href: d.href }) }
    else if (d.action === "scroll") {
      const el = document.getElementById(d.id) ?? document.getElementById(d.rawId)
      if (el) { e.preventDefault(); el.scrollIntoView({ block: "start" }); el.tabIndex = -1; el.focus({ preventScroll: true }); history.replaceState(null, "", `#${d.rawId}`) }
    }
  }

  if (doc.html === null) return null
  return (
    // the wrapper only delegates clicks; links inside stay real <a> elements (keyboard activation fires click too)
    // eslint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-static-element-interactions
    <div onClick={onClick} data-doc-id={doc.id}>
      <SafeHtml ref={ref} html={doc.html} className="prose dark:prose-invert docs max-w-none" {...(doc.renderedBy === "github" ? { lang: "en" } : {})} />
    </div>
  )
}
