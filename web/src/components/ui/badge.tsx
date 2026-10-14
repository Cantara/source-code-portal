import type { HTMLAttributes } from "react"
import { cn } from "@/lib/cn"

const variants = {
  default: "border-border bg-secondary text-secondary-foreground",
  ok: "border-emerald-700/30 bg-emerald-50 text-emerald-900 dark:bg-emerald-950 dark:text-emerald-100",
  warn: "border-amber-700/30 bg-amber-50 text-amber-900 dark:bg-amber-950 dark:text-amber-100",
  error: "border-red-700/30 bg-red-50 text-red-900 dark:bg-red-950 dark:text-red-100",
}

export function Badge({
  variant = "default",
  className,
  ...props
}: HTMLAttributes<HTMLSpanElement> & { variant?: keyof typeof variants }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs font-medium whitespace-nowrap",
        variants[variant],
        className,
      )}
      {...props}
    />
  )
}
