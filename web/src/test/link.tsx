import type { AnchorHTMLAttributes } from "react"

// A stand-in for next/link in component tests: a plain anchor.
export default function Link({ href, ...rest }: AnchorHTMLAttributes<HTMLAnchorElement> & { href: string }) {
  return <a href={href} {...rest} />
}
