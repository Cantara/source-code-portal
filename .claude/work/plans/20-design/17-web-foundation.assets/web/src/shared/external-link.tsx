import { t } from "@lingui/core/macro"
import { ExternalLink as Icon } from "lucide-react"
import { cn } from "cn"
import type { ReactNode } from "react"
import { isWebUrl } from "@/lib/url"

/** Off-site link: opens in a new tab, says so to assistive technology, never leaks the referrer path. */
export function ExternalLink({ href, children, className }: { href: string; children: ReactNode; className?: string | undefined }) {
  if (!isWebUrl(href)) return <span className={className}>{children}</span>
  return (
    <a href={href} target="_blank" rel="noreferrer noopener" className={cn("inline-flex items-center gap-1 underline", className)}>
      {children}
      <Icon aria-hidden="true" className="size-3" />
      <span className="sr-only">{t`(opens in a new tab)`}</span>
    </a>
  )
}
