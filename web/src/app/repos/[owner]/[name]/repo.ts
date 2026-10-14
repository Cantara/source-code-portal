import { permanentRedirect } from "next/navigation"
import { repoQuery } from "@/features/repos/api"
import type { Api } from "@/lib/api/client"
import { load, peek } from "@/lib/load"

/**
 * loadRepo loads the repository of a page: unknown is the not-found page, and a renamed or transferred repository (the API followed
 * the alias) answers 308 to its current address, with rest (the page under the repository) appended.
 */
export async function loadRepo(api: Api, owner: string, name: string, rest = "") {
  const data = await load(repoQuery(api, owner, name))
  if (data && data.repo.address !== `${owner}/${name}`.toLowerCase()) {
    permanentRedirect(`/repos/${encodeURIComponent(data.repo.owner)}/${encodeURIComponent(data.repo.name)}${rest}`)
  }
  return data
}

/** peekRepo is the repository for the metadata of its pages, undefined when it cannot be read. */
export async function peekRepo(api: Api, owner: string, name: string) {
  return peek(repoQuery(api, owner, name))
}
