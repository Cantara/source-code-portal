import { meQuery } from "@/lib/api/queries"
import { load } from "@/lib/load"
import type { Route, Search } from "@/lib/page"
import { SignInView } from "./view"

const one = (v: Search[string]) => (Array.isArray(v) ? v[0] : v)

/** signInRoute is `/signin`: the provider's button and the token form, with what went wrong at the last attempt. */
export const signInRoute: Route<Record<string, never>> = {
  load: ({ api, qc }) => load(qc, meQuery(api)),
  metadata: () => ({ title: "Sign in", robots: { index: false, follow: false } }),
  render: ({ search }) => <SignInView error={one(search.error)} returnTo={one(search.return_to)} />,
}
