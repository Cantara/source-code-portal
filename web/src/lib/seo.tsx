import "server-only"
import type { Metadata } from "next"
import { headers } from "next/headers"
import type { Schemas } from "./api/client"
import type { Crumb } from "./crumbs"
import { repoPath } from "./paths"

/** Thing is one Schema.org node of a page's JSON-LD graph. */
export type Thing = Record<string, unknown> & { "@type": string }

/** nonce is the request's CSP nonce, from the policy the Go server sets on the page request. */
async function nonce(): Promise<string | undefined> {
  return /'nonce-([^']+)'/.exec((await headers()).get("content-security-policy") ?? "")?.[1]
}

/** serialise is the graph as a script's text: `<` escaped, so no text inside can end the script element. */
export function serialise(graph: Thing[]): string {
  return JSON.stringify({ "@context": "https://schema.org", "@graph": graph }).replace(/</g, "\\u003c")
}

/** JsonLd is a page's structured data: one graph, a data block carrying the page's nonce so the CSP stays uniform. */
export async function JsonLd({ graph }: { graph: Thing[] }) {
  return <script type="application/ld+json" nonce={await nonce()} dangerouslySetInnerHTML={{ __html: serialise(graph) }} />
}

/** The JSON-LD nodes of the pages, with absolute URLs and @ids under base (the portal's origin). */
export const ld = {
  website: (base: string): Thing => ({
    "@type": "WebSite",
    "@id": `${base}/#website`,
    url: `${base}/`,
    name: "Source Code Portal",
    description: "Repositories, documentation and commits of the organisation.",
  }),

  breadcrumbs: (base: string, trail: Crumb[]): Thing => ({
    "@type": "BreadcrumbList",
    "@id": `${base}${trail.at(-1)?.path ?? "/"}#breadcrumbs`,
    itemListElement: trail.map((c, i) => ({ "@type": "ListItem", position: i + 1, name: c.name, item: `${base}${c.path}` })),
  }),

  /** page is the page itself; trail says it has a BreadcrumbList (every page but the dashboard) */
  page: (base: string, path: string, name: string, type = "WebPage", trail = true): Thing => ({
    "@type": type,
    "@id": `${base}${path}`,
    url: `${base}${path}`,
    name,
    isPartOf: { "@id": `${base}/#website` },
    ...(trail && { breadcrumb: { "@id": `${base}${path}#breadcrumbs` } }),
  }),

  /** repo is a repository's SoftwareSourceCode: its code on GitHub, language, SPDX licence, newest commit and latest release. */
  repo: (base: string, path: string, d: Schemas["RepoDetail"]): Thing => {
    const r = d.repo
    return {
      "@type": "SoftwareSourceCode",
      "@id": `${base}${path}#code`,
      name: d.title || `${r.owner}/${r.name}`,
      ...((d.descriptor.description || r.description) && { description: d.descriptor.description || r.description }),
      url: `${base}${path}`,
      codeRepository: r.htmlUrl,
      ...(r.language && { programmingLanguage: r.language }),
      ...(r.license && r.license !== "NOASSERTION" && { license: `https://spdx.org/licenses/${r.license}.html` }),
      ...(r.lastCommitAt && { dateModified: r.lastCommitAt }),
      ...(r.release && { version: r.release.tag }),
      ...(r.topics.length > 0 && { keywords: r.topics.join(", ") }),
      author: { "@type": "Organization", name: r.owner, url: `https://github.com/${r.owner}` },
      mainEntityOfPage: { "@id": `${base}${path}` },
    }
  },

  /** group is a group's page: a CollectionPage whose main entity is the list of its repositories. */
  group: (base: string, path: string, g: Schemas["Group"]): Thing => ({
    ...ld.page(base, path, g.name, "CollectionPage"),
    ...(g.description && { description: g.description }),
    mainEntity: {
      "@type": "ItemList",
      numberOfItems: g.repos.length,
      itemListElement: g.repos.map((r, i) => ({
        "@type": "ListItem",
        position: i + 1,
        name: `${r.owner}/${r.name}`,
        url: `${base}${repoPath(r.owner, r.name)}`,
      })),
    },
  }),

  /** doc is a document's TechArticle, part of its repository's code. */
  doc: (base: string, path: string, owner: string, name: string, d: Schemas["Doc"]): Thing => ({
    "@type": "TechArticle",
    "@id": `${base}${path}#article`,
    headline: d.title || d.path,
    ...(d.description && { description: d.description }),
    url: `${base}${path}`,
    isPartOf: { "@id": `${base}${repoPath(owner, name)}#code` },
    mainEntityOfPage: { "@id": `${base}${path}` },
  }),
}

const site = "Source Code Portal"

/** titleOf is a page's own title, without the site's suffix. */
function titleOf(m: Metadata): string {
  const t = m.title
  if (typeof t === "string") return t
  if (t && "absolute" in t && t.absolute) return t.absolute.replace(` · ${site}`, "")
  return site
}

/** social adds a page's Open Graph and X summary (title, description, canonical address) for the previews of shared links. */
export function social(m: Metadata): Metadata {
  const title = titleOf(m)
  const description = typeof m.description === "string" ? m.description : undefined
  const canonical = m.alternates?.canonical
  const url = typeof canonical === "string" ? canonical : undefined
  return {
    ...m,
    openGraph: { type: "website", siteName: site, title, description, url },
    twitter: { card: "summary", title, description },
  }
}
