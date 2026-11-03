import { cn } from "@/lib/cn"

/**
 * Sparkline draws counts (oldest first) as bars, scaled to the largest; part, when given, is the share of each bar drawn at its foot
 * in a second colour (the commits with an AI). Its label says what they count, so the picture is never the only way to read it.
 */
export function Sparkline({
  values,
  part,
  label,
  className,
}: {
  values: number[]
  part?: number[]
  label: string
  className?: string
}) {
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
        const p = Math.min(part?.[i] ?? 0, v)
        const foot = p === 0 ? 0 : Math.max(1, Math.round((p / v) * bar))
        return (
          <g key={i}>
            <rect x={i * (w + gap)} y={h - bar} width={w} height={bar} rx={1} fill="currentColor" opacity={v === 0 ? 0.25 : 1} />
            {foot > 0 && <rect x={i * (w + gap)} y={h - foot} width={w} height={foot} rx={1} fill="var(--ai)" />}
          </g>
        )
      })}
    </svg>
  )
}
