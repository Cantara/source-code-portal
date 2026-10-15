import { describe, expect, it } from "vitest"
import { repo } from "@/test/server"
import { crumbs } from "./crumbs"
import { ld, serialise } from "./seo"

const base = "https://scp.example"

describe("JSON-LD", () => {
  it("cannot end its script element", () => {
    const text = serialise([{ "@type": "Thing", name: "</script><script>alert(1)</script>" }])
    expect(text).not.toContain("<")
    expect(JSON.parse(text)["@graph"][0].name).toBe("</script><script>alert(1)</script>")
  })

  it("describes a repository with its code, licence, newest commit and release", () => {
    const detail = {
      repo: repo("Core", { license: "Apache-2.0", topics: ["iam", "java"] }),
      title: "Core",
      descriptor: { description: "", lifecycle: "", owners: [], tags: [], links: [] },
      commits: [],
      commitsFreshness: repo("Core").freshness,
    } as unknown as Parameters<typeof ld.repo>[2]
    expect(ld.repo(base, "/repos/acme/core", detail)).toMatchObject({
      "@type": "SoftwareSourceCode",
      "@id": `${base}/repos/acme/core#code`,
      codeRepository: "https://github.com/acme/Core",
      license: "https://spdx.org/licenses/Apache-2.0.html",
      version: "v1.0.0",
      programmingLanguage: "Go",
      keywords: "iam, java",
      author: { "@type": "Organization", name: "acme", url: "https://github.com/acme" },
    })
  })

  it("gives the trail it draws", () => {
    const trail = crumbs.doc("acme", "Core", "docs/setup.md", "Setup")
    expect(ld.breadcrumbs(base, trail)).toEqual({
      "@type": "BreadcrumbList",
      "@id": `${base}/repos/acme/core/docs/docs/setup#breadcrumbs`,
      itemListElement: [
        { "@type": "ListItem", position: 1, name: "Dashboard", item: `${base}/` },
        { "@type": "ListItem", position: 2, name: "acme/Core", item: `${base}/repos/acme/core` },
        { "@type": "ListItem", position: 3, name: "Setup", item: `${base}/repos/acme/core/docs/docs/setup` },
      ],
    })
  })
})
