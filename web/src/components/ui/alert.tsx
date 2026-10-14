import type { HTMLAttributes } from "react"
import { cn } from "@/lib/cn"

const variants = {
  default: "border-border bg-card",
  warn: "border-amber-700/40 bg-amber-50 text-amber-950 dark:bg-amber-950 dark:text-amber-50",
  error: "border-red-700/40 bg-red-50 text-red-950 dark:bg-red-950 dark:text-red-50",
}

export function Alert({
  variant = "default",
  className,
  ...props
}: HTMLAttributes<HTMLDivElement> & { variant?: keyof typeof variants }) {
  return <div className={cn("rounded-lg border p-3 text-sm", variants[variant], className)} {...props} />
}
