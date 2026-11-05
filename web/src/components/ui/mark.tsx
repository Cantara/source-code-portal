import { cn } from "@/lib/cn"

/** Mark is the portal's brand mark: a hexagon holding a small graph, in the primary colour with its glow. */
export function Mark({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 32 32"
      aria-hidden="true"
      className={cn("size-7 shrink-0 text-primary drop-shadow-[0_0_6px_var(--glow)]", className)}
    >
      <path d="M16 2.5 27.7 9.25v13.5L16 29.5 4.3 22.75V9.25Z" fill="none" stroke="currentColor" strokeWidth="1.6" />
      <path d="M16 9v7m0 0-6 4m6-4 6 4" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
      <circle cx="16" cy="9" r="2.2" fill="currentColor" />
      <circle cx="10" cy="20" r="2.2" fill="currentColor" />
      <circle cx="22" cy="20" r="2.2" fill="currentColor" />
    </svg>
  )
}
