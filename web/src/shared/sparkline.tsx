import { cn } from "@/lib/cn"

/**
 * Sparkline draws counts (oldest first) as bars, scaled to the largest; its label says what they count and the last value, so the
 * picture is never the only way to read it.
 */
export function Sparkline({ values, label, className }: { values: number[]; label: string; className?: string }) {
  const top = Math.max(1, ...values)
  const w = 4
  const gap = 1
  const h = 20
  return (
    <svg
      role="img"
      aria-label={label}
      viewBox={`0 0 ${values.length * (w + gap) - gap} ${h}`}
      className={cn("h-5 w-[3.75rem] shrink-0 text-primary", className)}
      preserveAspectRatio="none"
    >
      <title>{label}</title>
      {values.map((v, i) => {
        const bar = v === 0 ? 1 : Math.max(2, Math.round((v / top) * h))
        return (
          <rect
            key={i}
            x={i * (w + gap)}
            y={h - bar}
            width={w}
            height={bar}
            rx={1}
            fill="currentColor"
            opacity={v === 0 ? 0.25 : 1}
          />
        )
      })}
    </svg>
  )
}
