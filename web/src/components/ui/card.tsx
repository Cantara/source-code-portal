import type { HTMLAttributes } from "react"
import { cn } from "@/lib/cn"

export function Card({ className, ...props }: HTMLAttributes<HTMLElement>) {
  return <section className={cn("panel p-4", className)} {...props} />
}

export function CardTitle({ className, ...props }: HTMLAttributes<HTMLHeadingElement>) {
  return <h2 className={cn("text-lg font-semibold", className)} {...props} />
}
