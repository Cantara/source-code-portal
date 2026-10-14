import { memo } from "react"

/**
 * SafeHtml renders HTML the server sanitised; it is the only place the UI sets markup. Memoised on the string, so a refetch with equal
 * content keeps the DOM and the scroll position.
 */
export const SafeHtml = memo(function SafeHtml({ html, className }: { html: string; className?: string }) {
  return <div className={className} dangerouslySetInnerHTML={{ __html: html }} />
})
