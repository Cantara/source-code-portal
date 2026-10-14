import type { Schemas } from "@/lib/api/client"

/** readmeOf is the README of a document list: the server lists it first and marks it. */
export function readmeOf(list: Schemas["DocList"] | undefined) {
  return list?.docs.find((d) => d.readme)
}
