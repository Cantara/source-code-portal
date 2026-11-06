import { cn } from "@/lib/cn"

/** A state as the queues tell it: its words, what it waits for and the colour of its dot (a theme token class). */
export type StateWords = { label: string; waits: string; dot: string }

/** StateChip is where a pull request or an issue stands: its dot and its words, with what it waits for for a screen reader and on hover. */
export function StateChip({ state, className }: { state: StateWords; className?: string }) {
  return (
    <span
      title={`${state.label}: ${state.waits}`}
      className={cn(
        "inline-flex shrink-0 items-center gap-1.5 rounded-full border border-border px-2 py-px text-xs font-medium whitespace-nowrap text-foreground",
        className,
      )}
    >
      <span aria-hidden="true" className={cn("size-1.5 rounded-full", state.dot)} />
      {state.label}
      <span className="sr-only">, {state.waits}</span>
    </span>
  )
}
