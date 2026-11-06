import { latestCommitsPanel } from "@/features/commits/panels"
import { dependenciesPanel, technologyPanel } from "@/features/manifests/panels"
import { packagesPanel } from "@/features/packages/panels"
import { repoIssuesPanel } from "@/features/issues/panels"
import { repoPullsPanel } from "@/features/pulls/panels"
import { repoReleasesPanel } from "@/features/releases/panels"

/** panels are what other features contribute to the repository page, in order within their column. */
export const panels = [
  dependenciesPanel,
  technologyPanel,
  packagesPanel,
  repoPullsPanel,
  repoIssuesPanel,
  repoReleasesPanel,
  latestCommitsPanel,
]
