// The ONLY import surface of the `docs` slice for other slices and for `app/` (architecture 12.2: other features only through index.ts).
// The companion tree exports the pure (cc) pieces only, so node-environment unit tests can import it (api.ts reaches lib/api/client.ts, which reads
// `window.location` at load). The real file also exports docQuery and repoQuery from ./api and the page components (RepoLayout, RepoOverview, DocPage) from ./route.
export { docSplat, resolveDocId, stripDocExt } from "./docId"
