/** repoBadges are the badges every repository has, in display order (/badges/repos/{owner}/{name}/{kind}.svg). */
export const repoBadges = ["last-commit", "release", "license", "issues"] as const

/** RepoBadges shows a repository's SVG badges. */
export function RepoBadges({ address }: { address: string }) {
  return (
    <span className="flex flex-wrap gap-1.5">
      {repoBadges.map((b) => (
        <img key={b} src={`/badges/repos/${address}/${b}.svg`} alt={`${b.replace("-", " ")} badge`} height={20} className="h-5" />
      ))}
    </span>
  )
}
