import { latestCommitsPanel } from "@/features/commits/panels"
import { dependenciesPanel, technologyPanel } from "@/features/manifests/panels"
import { packagesPanel } from "@/features/packages/panels"

/** panels are what other features contribute to the repository page, in order within their column. */
export const panels = [dependenciesPanel, technologyPanel, packagesPanel, latestCommitsPanel]
