import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Alert } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Card, CardTitle } from "@/components/ui/card"
import type { Schemas } from "@/lib/api/client"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { ProblemAlert } from "@/shared/problem-alert"
import { manifestsQuery } from "./api"

/** at is the route parameters of a repository address. */
function at(address: string) {
  const [owner = "", name = ""] = address.split("/")
  return { owner, name }
}

/** TechnologyCard shows what the repository's manifests state, and the catalogued repositories it uses and is used by. */
export function TechnologyCard({ owner, name }: { owner: string; name: string }) {
  const { data, error } = useQuery(manifestsQuery(owner, name))
  if (data && data.manifests.length === 0 && data.usedBy.length === 0) return null
  const t = data?.technology
  return (
    <Card aria-labelledby="technology">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <CardTitle id="technology">Technology</CardTitle>
        {data && <FreshnessBadge freshness={data.freshness} />}
      </div>
      {error && !data && <ProblemAlert error={error} />}
      {t && (
        <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
          <Chips label="Languages" values={t.languages} />
          <Chips label="Build tools" values={t.buildTools} />
          <Chips label="Toolchains" values={t.toolchains.map((c) => `${c.name} ${c.versions.join(", ")}`)} />
          <Repos label="Uses" links={data.uses} />
          <Repos label="Used by" links={data.usedBy} />
        </dl>
      )}
    </Card>
  )
}

function Chips({ label, values }: { label: string; values: string[] }) {
  if (values.length === 0) return null
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="flex flex-wrap gap-1">
        {values.map((v) => (
          <Badge key={v}>{v}</Badge>
        ))}
      </dd>
    </>
  )
}

function Repos({ label, links }: { label: string; links: Schemas["RepoLink"][] }) {
  if (links.length === 0) return null
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd>
        <ul>
          {links.map((l) => (
            <li key={l.address}>
              <Link
                to="/repos/$owner/$name"
                params={l.owner && l.name ? { owner: l.owner, name: l.name } : at(l.address)}
                className="hover:underline"
                title={l.via.join(", ")}
              >
                {l.owner && l.name ? `${l.owner}/${l.name}` : l.address}
              </Link>
            </li>
          ))}
        </ul>
      </dd>
    </>
  )
}

/** Dependencies lists every manifest with its declared dependencies; internal ones link to their repositories. */
export function Dependencies({ owner, name }: { owner: string; name: string }) {
  const { data } = useQuery(manifestsQuery(owner, name))
  if (!data || data.manifests.length === 0) return null
  return (
    <section aria-labelledby="dependencies" className="flex flex-col gap-3">
      <h2 id="dependencies" className="text-lg font-semibold">
        Dependencies
      </h2>
      {data.manifests.map((m) => (
        <details key={m.path} className="rounded-lg border border-border p-3" open={data.manifests.length === 1}>
          <summary className="cursor-pointer text-sm">
            <code>{m.path}</code> {m.name && <span className="text-muted-foreground">{m.name}</span>}{" "}
            <span className="text-muted-foreground">
              ({m.dependencies.length} {m.dependencies.length === 1 ? "dependency" : "dependencies"})
            </span>
          </summary>
          {m.problems.length > 0 && (
            <Alert variant="warn" className="mt-2">
              <ul className="list-disc pl-5">
                {m.problems.map((p) => (
                  <li key={p.pointer + p.code}>
                    {p.pointer && <code>{p.pointer}</code>} {p.message}
                  </li>
                ))}
              </ul>
            </Alert>
          )}
          {m.dependencies.length > 0 && <DependencyTable deps={m.dependencies} />}
        </details>
      ))}
    </section>
  )
}

function DependencyTable({ deps }: { deps: Schemas["Dependency"][] }) {
  return (
    <div className="mt-2 overflow-x-auto">
      <table className="w-full text-left text-sm">
        <thead className="text-muted-foreground">
          <tr>
            <th scope="col" className="py-1 pr-3 font-medium">
              Name
            </th>
            <th scope="col" className="py-1 pr-3 font-medium">
              Version
            </th>
            <th scope="col" className="py-1 font-medium">
              Scope
            </th>
          </tr>
        </thead>
        <tbody>
          {deps.map((d) => (
            <tr key={d.scope + d.name} className="border-t border-border">
              <td className="py-1 pr-3 break-all">
                {d.repos[0] ? (
                  <Link to="/repos/$owner/$name" params={at(d.repos[0])} className="underline">
                    {d.name}
                  </Link>
                ) : (
                  d.name
                )}
              </td>
              <td className="py-1 pr-3 break-all">
                <code>{d.version}</code>
              </td>
              <td className="py-1">{d.scope}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
