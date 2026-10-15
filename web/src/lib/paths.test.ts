import { describe, expect, it } from "vitest"
import cases from "./paths.cases.json"
import { docPath, groupPath, repoPath } from "./paths"

// internal/snapshot/pages_test.go holds Go's page addresses to the same cases.
describe("page paths", () => {
  it.each(cases.repos)("repository $owner/$name", ({ owner, name, path }) => expect(repoPath(owner, name)).toBe(path))
  it.each(cases.groups)("group $id", ({ id, path }) => expect(groupPath(id)).toBe(path))
  it.each(cases.docs)("document $doc", ({ owner, name, doc, path }) => expect(docPath(owner, name, doc)).toBe(path))
})
