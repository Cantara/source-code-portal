import { docListQuery, docQuery, repoQuery } from "@/lib/api/queries"
import { load, loadRepo, peek, prefetch } from "@/lib/load"
import type { Route } from "@/lib/page"
import { docPath } from "@/lib/paths"
import { DocView } from "./view"
import { crumbs } from "@/lib/crumbs"
import { ld } from "@/lib/seo"

type Params = { owner: string; name: string; path: string[] }

/** docRoute is `/repos/{owner}/{name}/docs/{path}`: one document, at its repository's current address. */
export const docRoute: Route<Params> = {
  async load({ api, qc, params: { owner, name, path } }) {
    const p = path.join("/")
    // The repository first: a renamed one redirects before its document is asked for.
    await loadRepo(api, qc, owner, name, (o, n) => docPath(o, n, p))
    await Promise.all([load(qc, docQuery(api, owner, name, p)), prefetch(qc, docListQuery(api, owner, name))])
  },
  async metadata({ api, qc, params: { owner, name, path } }) {
    const p = path.join("/")
    const [repo, doc] = await Promise.all([peek(qc, repoQuery(api, owner, name)), peek(qc, docQuery(api, owner, name, p))])
    const o = repo?.repo.owner ?? owner
    const n = repo?.repo.name ?? name
    return {
      title: doc?.title || p,
      description: doc?.description || `${doc?.path ?? p} in ${o}/${n}.`,
      alternates: { canonical: docPath(o, n, doc?.path ?? p) },
    }
  },
  async structured({ api, qc, params: { owner, name, path } }, base) {
    const p = path.join("/")
    const [repo, doc] = await Promise.all([peek(qc, repoQuery(api, owner, name)), peek(qc, docQuery(api, owner, name, p))])
    if (!repo || !doc) return []
    const { owner: o, name: n } = repo.repo
    const page = docPath(o, n, doc.path)
    const title = doc.title || doc.path
    return [
      ld.page(base, page, title),
      ld.doc(base, page, o, n, doc),
      ld.breadcrumbs(base, crumbs.doc(o, n, repo.group, doc.path, title)),
    ]
  },
  render: ({ params: { owner, name, path } }) => <DocView owner={owner} name={name} path={path.join("/")} />,
}
