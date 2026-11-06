"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { useRouter, useSearchParams } from "next/navigation"
import type { ReactNode } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardTitle } from "@/components/ui/card"
import { api, type Schemas } from "@/lib/api/client"
import { insightsQuery } from "@/lib/api/queries"
import { crumbs } from "@/lib/crumbs"
import { count, plural } from "@/lib/format"
import { addressPath } from "@/lib/paths"
import { ProductField } from "@/shared/filter-fields"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { PageHeader } from "@/shared/page-header"
import { ProblemAlert } from "@/shared/problem-alert"
import { RelativeTime } from "@/shared/relative-time"
import { ActivityBars, Concentration, DependencyMatrix, RankedBars, ReleaseRhythm, VersionLadder, type Bar } from "./charts"

type Insights = Schemas["Insights"]

/**
 * InsightsView is what the portal's held data says about the fleet (design/insights.md): five cards, each a question with its answer
 * first and the detail below it. The charts are inline SVG from the server's numbers, and every figure is also a table or a sentence.
 * It names repositories, never people.
 */
export function InsightsView() {
  const group = useSearchParams().get("group") ?? ""
  const router = useRouter()
  const { data, error } = useQuery(insightsQuery(api, group))
  return (
    <>
      <PageHeader title="Insights" trail={crumbs.insights()} meta={data && <FreshnessBadge freshness={data.freshness} />}>
        <form
          action="/insights"
          className="flex flex-wrap items-end gap-3 text-sm"
          aria-label="Narrow the insights"
          onSubmit={(e) => {
            e.preventDefault()
            const g = new FormData(e.currentTarget).get("group")
            router.push(typeof g === "string" && g !== "" ? `/insights?group=${encodeURIComponent(g)}` : "/insights")
          }}
        >
          <ProductField value={group || undefined} />
          <Button type="submit" variant="outline">
            Apply
          </Button>
        </form>
      </PageHeader>
      {error && !data && <ProblemAlert error={error} />}
      {data && (
        <div className="flex flex-col gap-6">
          <Cadence data={data} />
          <Technology data={data} />
          <Libraries data={data} />
          <SingleAuthor data={data} />
          <Ageing data={data} />
        </div>
      )}
    </>
  )
}

function Insight({ id, question, answer, children }: { id: string; question: string; answer: ReactNode; children?: ReactNode }) {
  return (
    <Card aria-labelledby={id}>
      <CardTitle id={id}>{question}</CardTitle>
      <p className="mt-1 text-lg">{answer}</p>
      {children}
    </Card>
  )
}

const th = "px-2 py-1.5 text-left text-xs font-medium text-muted-foreground"
const td = "px-2 py-1.5"
const repoLink = (r: string) => (
  <Link href={addressPath(r)} className="underline-offset-2 hover:underline">
    {r}
  </Link>
)

function Table({
  caption,
  head,
  plain = false,
  children,
}: {
  caption: string
  head: string[]
  /** not scrolling: for the table only a screen reader meets */
  plain?: boolean
  children: ReactNode
}) {
  return (
    <div className={plain ? undefined : "mt-3 max-w-3xl overflow-x-auto"}>
      <table className="w-full text-sm">
        <caption className="sr-only">{caption}</caption>
        <thead>
          <tr>
            {head.map((h) => (
              <th key={h} scope="col" className={th}>
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-border">{children}</tbody>
      </table>
    </div>
  )
}

/** monthName is "Nov" for "2026-11" (UTC, English, as the page's other words). */
const monthName = (m: string) => new Date(`${m}-01T00:00:00Z`).toLocaleString("en", { month: "short", timeZone: "UTC" })

/** Bars is the stable releases per month as an SVG chart with each month named and each count written over its bar; the hidden table below holds the same numbers. */
function Bars({ months }: { months: Schemas["InsightMonth"][] }) {
  const top = Math.max(1, ...months.map((m) => m.releases))
  const w = 30
  return (
    <svg
      role="img"
      aria-label={`Stable releases per month, the last ${months.length} months: ${months.map((m) => `${m.month} ${m.releases}`).join(", ")}`}
      viewBox={`0 0 ${months.length * w} 84`}
      className="mt-3 h-auto w-full max-w-xl"
    >
      {months.map((m, i) => {
        const h = (m.releases / top) * 52
        return (
          <g key={m.month}>
            <rect
              x={i * w + 5}
              y={64 - h}
              width={w - 10}
              height={Math.max(h, m.releases > 0 ? 2 : 0.75)}
              className="fill-primary"
              opacity={m.releases > 0 ? 1 : 0.25}
            />
            {m.releases > 0 && (
              <text x={i * w + w / 2} y={60 - h} textAnchor="middle" className="fill-foreground text-[9px]">
                {m.releases}
              </text>
            )}
            <text x={i * w + w / 2} y={78} textAnchor="middle" className="fill-muted-foreground text-[8px]">
              {monthName(m.month)}
            </text>
          </g>
        )
      })}
    </svg>
  )
}

/** AsTable folds a graph's numbers into a table the reader opens: the graph leads, the exact values stay one click away. */
function AsTable({ children }: { children: ReactNode }) {
  return (
    <details className="mt-3 text-sm">
      <summary className="w-fit cursor-pointer text-muted-foreground hover:text-foreground">Show as a table</summary>
      {children}
    </details>
  )
}

function Cadence({ data }: { data: Insights }) {
  const { months, products } = data.cadence
  const total = months.reduce((n, m) => n + m.releases, 0)
  return (
    <Insight
      id="cadence"
      question="How often does the fleet ship?"
      answer={
        total === 0
          ? `No stable release in the last ${data.thresholds.months} months.`
          : `${plural(total, "stable release", "stable releases")} in the last ${data.thresholds.months} months.`
      }
    >
      <ReleaseRhythm products={products} />
      <h3 className="mt-5 text-sm font-medium">Per month</h3>
      <Bars months={months} />
      <div className="sr-only">
        <Table caption="Stable releases per month" head={["Month", "Stable releases"]} plain>
          {months.map((m) => (
            <tr key={m.month}>
              <td className={td}>{m.month}</td>
              <td className={td}>{m.releases}</td>
            </tr>
          ))}
        </Table>
      </div>
      <AsTable>
        <Table
          caption="Releases per product"
          head={["Product", "Stable releases held", `Median days between the last ${data.thresholds.releases}`, "Newest"]}
        >
          {products.map((p) => (
            <tr key={p.group}>
              <td className={td}>
                <Link href={`/groups/${encodeURIComponent(p.group)}`} className="underline-offset-2 hover:underline">
                  {p.name}
                </Link>
              </td>
              <td className={td}>{p.releases === 0 ? "None released" : count(p.releases)}</td>
              <td className={td}>{p.medianDays === null || p.medianDays === undefined ? "–" : p.medianDays}</td>
              <td className={td}>{p.last ? <RelativeTime at={p.last} /> : "–"}</td>
            </tr>
          ))}
        </Table>
      </AsTable>
      <p className="mt-2 text-xs text-muted-foreground">
        A product that ships weekly is not better than one that ships quarterly. Only the 20 newest releases of each repository
        are held, so the weeks before that are empty for a repository that ships often.
      </p>
    </Insight>
  )
}

const share = (c: Schemas["InsightCount"]): Bar => ({
  label: c.name,
  value: c.repos,
  text: `${count(c.repos)} · ${Math.round(c.share * 100)}%`,
  hint: `${c.name}: ${plural(c.repos, "repository", "repositories")}, ${Math.round(c.share * 100)}% of the fleet`,
})

function Counts({ title, rows }: { title: string; rows: Schemas["InsightCount"][] }) {
  return (
    <Table caption={title} head={[title, "Repositories", "Share"]}>
      {rows.map((r) => (
        <tr key={r.name}>
          <td className={td}>{r.name}</td>
          <td className={td}>{count(r.repos)}</td>
          <td className={td}>{Math.round(r.share * 100)}%</td>
        </tr>
      ))}
    </Table>
  )
}

function Technology({ data }: { data: Insights }) {
  const t = data.technology
  const first = t.languages[0]
  const bars = (rows: Schemas["InsightCount"][]) => rows.slice(0, 8).map((c, i) => ({ ...share(c), emphasis: i === 0 }))
  return (
    <Insight
      id="technology"
      question="What is the fleet built on?"
      answer={
        first
          ? `${first.name} in ${plural(first.repos, "repository", "repositories")}, ${Math.round(first.share * 100)}% of the fleet.`
          : "No manifest names a language."
      }
    >
      {t.languages.length > 0 && (
        <>
          <h3 className="mt-4 text-sm font-medium">Languages</h3>
          <RankedBars
            label="Repositories by language"
            bars={bars(t.languages)}
            max={Math.max(1, ...t.languages.map((l) => l.repos))}
          />
        </>
      )}
      {t.buildTools.length > 0 && (
        <>
          <h3 className="mt-4 text-sm font-medium">Build tools</h3>
          <RankedBars
            label="Repositories by build tool"
            bars={bars(t.buildTools)}
            max={Math.max(1, ...t.buildTools.map((l) => l.repos))}
          />
        </>
      )}
      {t.runtimes.length > 0 && (
        <>
          <h3 className="mt-5 text-sm font-medium">Runtime versions the manifests name</h3>
          <p className="text-xs text-muted-foreground">
            The oldest version of each runtime is marked: where the fleet is behind.
          </p>
          {t.runtimes.map((r) => (
            <VersionLadder key={r.name} runtime={r} />
          ))}
        </>
      )}
      <AsTable>
        {t.languages.length > 0 && <Counts title="Language" rows={t.languages} />}
        {t.buildTools.length > 0 && <Counts title="Build tool" rows={t.buildTools} />}
        {t.runtimes.length > 0 && (
          <Table caption="Runtimes the manifests name" head={["Runtime", "Repositories", "Oldest", "Newest"]}>
            {t.runtimes.map((r) => (
              <tr key={r.name}>
                <td className={td}>{r.name}</td>
                <td className={td}>{count(r.repos)}</td>
                <td className={td}>{r.oldest}</td>
                <td className={td}>{r.newest}</td>
              </tr>
            ))}
          </Table>
        )}
      </AsTable>
      <p className="mt-2 text-xs text-muted-foreground">
        Only what manifests say; {plural(t.unknown, "repository", "repositories")} named no language.
      </p>
    </Insight>
  )
}

function Libraries({ data }: { data: Insights }) {
  const libs = data.libraries
  const first = libs[0]
  const retired = new Set(libs.filter((l) => l.retired).map((l) => l.repo))
  return (
    <Insight
      id="libraries"
      question="What does the fleet lean on?"
      answer={
        first ? (
          <>
            {repoLink(first.repo)} has {plural(first.dependants, "dependant", "dependants")}: the widest reach of a change.
          </>
        ) : (
          "No repository depends on another."
        )
      }
    >
      {libs.length > 0 && (
        <>
          <RankedBars
            label="Dependants per library"
            bars={libs.map((l, i) => ({
              label: l.repo.slice(l.repo.indexOf("/") + 1) + (l.retired ? " (retired)" : ""),
              value: l.dependants,
              text: count(l.dependants),
              hint: `${l.repo}: ${plural(l.dependants, "repository depends", "repositories depend")} on it${l.retired ? `; it is ${l.retired} and still used` : ""}`,
              emphasis: i === 0,
            }))}
          />
          <h3 className="mt-5 text-sm font-medium">Who leans on which</h3>
          <DependencyMatrix matrix={data.matrix} retired={retired} />
          <AsTable>
            <Table caption="Libraries with the most dependants" head={["Library", "Dependants", "Latest release", "Used by"]}>
              {libs.map((l) => (
                <tr key={l.repo}>
                  <td className={td}>
                    {repoLink(l.repo)}
                    {l.retired && (
                      <span className="ml-2 rounded-md border border-border px-1.5 text-xs">
                        {l.retired === "archived" ? "Archived" : "Deprecated"}, still used
                      </span>
                    )}
                  </td>
                  <td className={td}>{count(l.dependants)}</td>
                  <td className={td}>{l.release ?? "–"}</td>
                  <td className={td}>
                    {l.dependantRepos.map((r, i) => (
                      <span key={r}>
                        {i > 0 && ", "}
                        {repoLink(r)}
                      </span>
                    ))}
                    {l.dependants > l.dependantRepos.length && ` and ${l.dependants - l.dependantRepos.length} more`}
                  </td>
                </tr>
              ))}
            </Table>
          </AsTable>
        </>
      )}
    </Insight>
  )
}

function SingleAuthor({ data }: { data: Insights }) {
  const rows = data.singleAuthor
  const t = data.thresholds
  const judged = data.concentration.reduce((n, b) => n + b.repos, 0)
  return (
    <Insight
      id="single-author"
      question="Where does knowledge sit with one person?"
      answer={
        rows.length === 0
          ? "No repository is mostly one person's."
          : `${plural(rows.length, "repository", "repositories")} where one person wrote ${t.singleAuthorShare}% or more of the people's recent commits.`
      }
    >
      {judged > 0 && (
        <>
          <p className="mt-1 text-xs text-muted-foreground">
            How much of the people&apos;s recent commits the busiest author wrote, for the{" "}
            {plural(judged, "repository", "repositories")} with at least {t.singleAuthorCommits} of them held.
          </p>
          <Concentration bands={data.concentration} threshold={t.singleAuthorShare} />
        </>
      )}
      {rows.length > 0 && (
        <ul aria-label="Repositories mostly one person's" className="mt-3 divide-y divide-border text-sm">
          {rows.map((r) => (
            <li key={r.repo} className="flex flex-wrap items-baseline gap-x-3 py-1.5">
              {repoLink(r.repo)}
              <span className="text-muted-foreground">{count(r.commits)} commits by people held</span>
            </li>
          ))}
        </ul>
      )}
      <p className="mt-2 text-xs text-muted-foreground">
        Places to share the knowledge, not people to watch: the page never says who. Automation and sweeps do not count.
      </p>
    </Insight>
  )
}

function Ageing({ data }: { data: Insights }) {
  const a = data.ageing
  return (
    <Insight
      id="ageing"
      question="What has gone quiet?"
      answer={
        a.dormant === 0
          ? "No repository is dormant."
          : `${plural(a.dormant, "repository is", "repositories are")} dormant, candidates for retirement.`
      }
    >
      <ActivityBars total={a} products={a.products} />
      {a.dormantRepos.length > 0 && (
        <ul aria-label="Dormant repositories" className="mt-3 divide-y divide-border text-sm">
          {a.dormantRepos.map((r) => (
            <li key={r.repo} className="flex flex-wrap items-baseline gap-x-3 py-1.5">
              {repoLink(r.repo)}
              <span className="text-muted-foreground">
                last changed <RelativeTime at={r.lastChange} />
              </span>
            </li>
          ))}
        </ul>
      )}
      <AsTable>
        <Table caption="Repositories by activity" head={["Active", "Recent", "Quiet", "Dormant"]}>
          <tr>
            <td className={td}>{count(a.active)}</td>
            <td className={td}>{count(a.recent)}</td>
            <td className={td}>{count(a.quiet)}</td>
            <td className={td}>{count(a.dormant)}</td>
          </tr>
        </Table>
      </AsTable>
      <p className="mt-2 text-xs text-muted-foreground">
        Already retired ones are on{" "}
        <Link href="/alumni" className="underline-offset-2 hover:underline">
          Alumni
        </Link>
        .
      </p>
    </Insight>
  )
}
