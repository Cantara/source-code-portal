import { Mark } from "./mark"

/** Logo is the deployment's logo when its theme has one, else the portal's own mark. The picture is decorative: the name beside it says it. */
export function Logo({ src, className }: { src?: string; className?: string }) {
  if (!src) return <Mark className={className} />
  return <img src={src} alt="" className={className ?? "size-7 shrink-0 object-contain"} />
}
