import type { AnchorHTMLAttributes } from "react"

// A stand-in for next/link in component tests: a plain anchor (the prefetch hint has no meaning there).
export default function Link({
  href,
  // eslint-disable-next-line @typescript-eslint/no-unused-vars -- taken out so it is not an attribute of the anchor
  prefetch: _prefetch,
  ...rest
}: AnchorHTMLAttributes<HTMLAnchorElement> & { href: string; prefetch?: boolean }) {
  return <a href={href} {...rest} />
}
