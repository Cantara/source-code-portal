import { docAddress } from "./docs"

const seg = encodeURIComponent

/**
 * The page addresses of the UI, each segment encoded: the one builder of page paths (links, canonicals, JSON-LD, breadcrumbs).
 * A repository's is its lower-case address, as Go writes it (internal/snapshot/pages.go; both are held to paths.cases.json).
 */
export const groupPath = (group: string) => `/groups/${seg(group)}`
export const groupCommitsPath = (group: string) => `${groupPath(group)}/commits`
export const repoPath = (owner: string, name: string) => `/repos/${seg(owner.toLowerCase())}/${seg(name.toLowerCase())}`
export const repoCommitsPath = (owner: string, name: string) => `${repoPath(owner, name)}/commits`

/** docPath is a document's page: the repository path without its extension, encoded per segment. */
export const docPath = (owner: string, name: string, path: string) =>
  `${repoPath(owner, name)}/docs/${docAddress(path).split("/").map(seg).join("/")}`

/** addressPath is the page of a repository address "owner/name". */
export const addressPath = (address: string) => repoPath(repoAt(address).owner, repoAt(address).name)

/** repoAt is the owner and name of an address "owner/name". */
export function repoAt(address: string): { owner: string; name: string } {
  const [owner = "", name = ""] = address.split("/")
  return { owner, name }
}
