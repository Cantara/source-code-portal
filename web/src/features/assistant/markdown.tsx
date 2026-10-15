import Link from "next/link"
import Markdown, { type Components } from "react-markdown"
import remarkGfm from "remark-gfm"

/** safeHref keeps links of the portal (relative paths) and https links; anything else is dropped. */
export function safeHref(href: string | undefined): string | undefined {
  if (!href) return undefined
  if (href.startsWith("/") && !href.startsWith("//") && !href.includes("\\")) return href
  try {
    return new URL(href).protocol === "https:" ? href : undefined
  } catch {
    return undefined
  }
}

const components: Components = {
  a: ({ href, children }) => {
    const to = safeHref(href)
    if (!to) return <span>{children}</span>
    return to.startsWith("/") ? (
      <Link href={to}>{children}</Link>
    ) : (
      <a href={to} rel="noopener noreferrer nofollow" target="_blank">
        {children}
      </a>
    )
  },
  // A model's images would fetch from anywhere: the alternative text stands in.
  img: ({ alt }) => <span>{alt}</span>,
}

/** Answer renders a model's Markdown: no HTML (react-markdown escapes it), no images, links of the portal or https only. */
export function Answer({ text }: { text: string }) {
  return (
    <div className="doc text-sm">
      <Markdown remarkPlugins={[remarkGfm]} components={components} skipHtml>
        {text}
      </Markdown>
    </div>
  )
}
