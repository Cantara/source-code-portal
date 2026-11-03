import { cn } from "@/lib/cn"

/**
 * Sparkline draws weekly counts (oldest first) as bars scaled to the largest, each bar stacking its parts from the foot (the kinds of
 * work, each in its fill). Its label says what they count, so the picture is never the only way to read it.
 */
export function Sparkline({
  parts,
  label,
  className,
}: {
  parts: { values: number[]; fill: string }[]
  label: string
  className?: string
}) {
  const weeks = Math.max(0, ...parts.map((p) => p.values.length))
  const totals = Array.from({ length: weeks }, (_, i) => parts.reduce((n, p) => n + (p.values[i] ?? 0), 0))
  const top = Math.max(1, ...totals)
  const w = 4
  const gap = 1
  const h = 20
  return (
    <svg
      role="img"
      aria-label={label}
      viewBox={`0 0 ${weeks * (w + gap) - gap} ${h}`}
      className={cn("h-5 w-[3.75rem] shrink-0 text-primary", className)}
      preserveAspectRatio="none"
    >
      <title>{label}</title>
      {totals.map((v, i) => {
        const x = i * (w + gap)
        if (v === 0) return <rect key={i} x={x} y={h - 1} width={w} height={1} rx={1} fill="currentColor" opacity={0.25} />
        const bar = Math.max(2, Math.round((v / top) * h))
        let below = 0 // the parts' count under this one
        return (
          <g key={i}>
            {parts.map((p, j) => {
              const n = p.values[i] ?? 0
              const from = Math.round((below / v) * bar)
              below += n
              const to = Math.round((below / v) * bar)
              return to > from && <rect key={j} x={x} y={h - to} width={w} height={to - from} rx={1} fill={p.fill} />
            })}
          </g>
        )
      })}
    </svg>
  )
}
