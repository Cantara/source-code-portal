"use client"

import { useMutation, useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { type ReactNode, useSyncExternalStore } from "react"
import { Alert } from "@/components/ui/alert"
import { Button, buttonClass } from "@/components/ui/button"
import { Card, CardTitle } from "@/components/ui/card"
import { api, get, ProblemError } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { assistantQuery, meQuery, prerequisitesQuery } from "@/lib/api/queries"
import { plural } from "@/lib/format"
import { PageHeader } from "@/shared/page-header"
import { PrerequisiteDetails, setupDocs } from "@/shared/prerequisites"
import { ProblemAlert } from "@/shared/problem-alert"
import { RelativeTime } from "@/shared/relative-time"
import { AnnouncementsCard } from "./announcements"
import { VisibilityCard } from "./visibility"
import { adminStatusQuery, testProvider, type AdminStatus } from "./api"

const via: Record<string, string> = { proxy: "the proxy", oidc: "single sign-on", token: "a token" }

function Facts({ rows }: { rows: [string, ReactNode][] }) {
  return (
    <dl className="mt-3 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
      {rows.map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="text-muted-foreground">{k}</dt>
          <dd>{v}</dd>
        </div>
      ))}
    </dl>
  )
}

const yes = (b: boolean) => (b ? "yes" : "no")

/** Warnings lists what the status says needs action. */
function Warnings({ s }: { s: AdminStatus }) {
  if (s.warnings.length === 0) return null
  return (
    <Alert variant="warn">
      <p className="font-semibold">{plural(s.warnings.length, "warning", "warnings")}</p>
      <ul className="mt-1 list-disc pl-5">
        {s.warnings.map((w) => (
          <li key={w}>{w}</li>
        ))}
      </ul>
    </Alert>
  )
}

/** Tile is one figure of the overview with what it means and where to act on it. */
function Tile({
  label,
  value,
  note,
  href,
  action,
  attention,
}: {
  label: string
  value: ReactNode
  note: ReactNode
  href?: string
  action?: string
  attention?: boolean
}) {
  return (
    <div className={cn("panel flex flex-col gap-1 p-4", attention && "border-amber-700/60")}>
      <span className="text-sm text-muted-foreground">{label}</span>
      <span className="text-2xl font-semibold">{value}</span>
      <span className="text-sm">{note}</span>
      {href && action && (
        <a href={href} className="mt-auto pt-1 text-sm text-primary hover:underline">
          {action}
        </a>
      )}
    </div>
  )
}

/** Overview leads with what needs action: the warnings, then a tile per area with its next step, then the instance. */
export function Overview({ s }: { s: AdminStatus }) {
  const { data: pre } = useQuery(prerequisitesQuery(api))
  const unmet = pre?.unmet.length ?? 0
  const stale = s.sources.filter((src) => src.freshness !== "fresh").length
  const hidden = s.snapshot.signedIn + s.snapshot.parked
  return (
    <div className="flex flex-col gap-4">
      <Warnings s={s} />
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Tile
          label="Repositories"
          value={s.snapshot.repos}
          note={
            hidden === 0
              ? "Everyone sees every one the access allows."
              : `${s.snapshot.signedIn} signed in only, ${s.snapshot.parked} no one.`
          }
          href="#visibility"
          action="Choose who sees what"
        />
        <Tile
          label="GitHub"
          value={unmet + stale === 0 ? "All set" : plural(unmet + stale, "to fix", "to fix")}
          note={`${plural(s.sources.length, "source", "sources")}${stale > 0 ? `, ${stale} not fresh` : ""}${unmet > 0 ? `, ${plural(unmet, "prerequisite", "prerequisites")} unmet` : ""}.`}
          href="#github"
          action="See sources and prerequisites"
          attention={unmet + stale > 0}
        />
        <Tile
          label="Events"
          value={s.webhook.enabled ? "On" : "Polling"}
          note={
            s.webhook.enabled ? (
              <>
                Last delivery <RelativeTime at={s.webhook.lastDelivery} />.
              </>
            ) : (
              "Pushes show at the next scheduled read."
            )
          }
          href="#github"
          action="See the webhook"
        />
        <Tile label="Jobs" value={s.queue.running} note={`running, ${s.queue.waiting} waiting.`} attention={!s.alive} />
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardTitle>Instance</CardTitle>
          <Facts
            rows={[
              ["Ready", yes(s.ready)],
              ["Sync engine alive", yes(s.alive)],
              ["Jobs waiting", s.queue.waiting],
              ["Jobs running", s.queue.running],
              [
                "Cluster",
                s.cluster.enabled
                  ? `${s.cluster.connected ? "connected" : "not connected"}, ${s.cluster.leader ? "leader" : "follower"}`
                  : "alone",
              ],
            ]}
          />
        </Card>
        <Card>
          <CardTitle>Snapshot</CardTitle>
          <Facts
            rows={[
              [
                "Version",
                <span key="v" className="font-mono">
                  {s.snapshot.version}
                </span>,
              ],
              ["Built", <RelativeTime key="b" at={s.snapshot.builtAt} />],
              ["Catalogued", yes(s.snapshot.catalogued)],
              ["Repositories", s.snapshot.repos],
              ["Signed in only", s.snapshot.signedIn],
              ["No one", s.snapshot.parked],
            ]}
          />
        </Card>
      </div>
    </div>
  )
}

/** GitHub is where the portal reads from: the sources, the webhook and what the features lack. */
export function GitHub({ s }: { s: AdminStatus }) {
  const cell = "px-3 py-1.5 first:pl-0 last:pr-0"
  return (
    <div className="flex flex-col gap-4">
      <Card aria-labelledby="sources">
        <CardTitle id="sources">Sources</CardTitle>
        <div className="mt-3 overflow-x-auto" tabIndex={0} role="region" aria-label="Sources table">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-muted-foreground">
                <th scope="col" className={cn(cell, "font-normal")}>
                  Source
                </th>
                <th scope="col" className={cn(cell, "text-right font-normal")}>
                  Repositories
                </th>
                <th scope="col" className={cn(cell, "font-normal")}>
                  Listing
                </th>
                <th scope="col" className={cn(cell, "font-normal")}>
                  Kept fresh
                </th>
                <th scope="col" className={cn(cell, "text-right font-normal")}>
                  Rate limit left
                </th>
              </tr>
            </thead>
            <tbody>
              {s.sources.map((src) => (
                <tr key={src.id} className="border-t border-border align-top">
                  <td className={cn(cell, "font-mono")}>{src.id}</td>
                  <td className={cn(cell, "text-right tabular-nums")}>{src.repos}</td>
                  <td className={cell}>
                    {src.freshness}, <RelativeTime at={src.checkedAt} />
                  </td>
                  <td className={cell}>
                    <span className="font-medium">{src.mode === "events" ? "Events" : "Polling"}</span>
                    <span className="block text-xs text-muted-foreground">{cadence(src)}</span>
                  </td>
                  <td className={cn(cell, "text-right tabular-nums")}>{Math.round(src.rateRemaining * 100)}%</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>
      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardTitle>Webhook</CardTitle>
          <Facts
            rows={[
              ["Events reach the portal", yes(s.webhook.enabled)],
              ["Receiver", yes(s.webhook.receiver)],
              ["Delivery log read", yes(s.webhook.catchUp)],
              ["Last delivery", <RelativeTime key="l" at={s.webhook.lastDelivery} />],
              ...Object.entries(s.webhook.deliveries)
                .sort(([a], [b]) => a.localeCompare(b))
                .map(([result, n]): [string, ReactNode] => [`Deliveries ${result}`, n]),
            ]}
          />
        </Card>
        <PrerequisitesCard />
      </div>
    </div>
  )
}

const subjects: Record<string, string> = {
  catalog: "Listing",
  commits: "Commits",
  pulls: "Pull requests",
  issues: "Issues",
  releases: "Releases",
  docs: "Documents",
  manifests: "Dependencies",
  packages: "Packages",
}

/** every is a refresh interval in words. */
function every(seconds: number): string {
  if (seconds < 3600) return `${Math.round(seconds / 60)} min`
  const h = seconds / 3600
  return `${Number.isInteger(h) ? h : h.toFixed(1)} h`
}

/** cadence tells how each subject of a source is refreshed: the net under events, the poll otherwise. */
function cadence(src: AdminStatus["sources"][number]): string {
  const parts = Object.entries(src.cadenceSeconds)
    .filter(([kind]) => subjects[kind])
    .map(([kind, s]) => `${subjects[kind]} ${every(s)}`)
  return (src.mode === "events" ? "net " : "every ") + parts.join(", ")
}

const access: Record<string, string> = { "signed-in": "signed-in viewers", admin: "administrators", everyone: "everyone" }

/** PrerequisitesCard lists what the features need from GitHub and lack (design/prerequisites.md), each with its fix. */
function PrerequisitesCard() {
  const { data, error } = useQuery(prerequisitesQuery(api))
  return (
    <Card aria-labelledby="prerequisites">
      <CardTitle id="prerequisites">GitHub prerequisites</CardTitle>
      {error && <ProblemAlert error={error} />}
      {data && data.unmet.length === 0 && (
        <p className="mt-1 text-sm text-muted-foreground">Every feature has what it needs from GitHub.</p>
      )}
      {data && data.unmet.length > 0 && (
        <ul className="mt-2 flex flex-col gap-3 text-sm">
          {data.unmet.map((p, i) => (
            <li key={`${p.feature}-${p.source ?? ""}-${i}`} className="flex flex-col gap-1">
              <p className="font-medium">
                {p.title}: {p.effect}
              </p>
              <PrerequisiteDetails p={p} />
            </li>
          ))}
        </ul>
      )}
      <a href={setupDocs} rel="noreferrer" className="mt-2 inline-block text-sm hover:underline">
        What the portal needs from GitHub
      </a>
    </Card>
  )
}

/** AssistantCard shows the assistant's provider and tests it with one short completion. */
export function AssistantCard() {
  const { data: a } = useQuery(assistantQuery(api))
  const test = useMutation({ mutationFn: testProvider })
  if (!a) return null
  const r = test.data
  return (
    <Card>
      <CardTitle>Assistant</CardTitle>
      <Facts
        rows={
          a.enabled
            ? [
                ["Provider", a.provider],
                [
                  "Model",
                  <span key="m" className="font-mono">
                    {a.model}
                  </span>,
                ],
                ["Open to", access[a.access] ?? a.access],
              ]
            : [["Configured", "no"]]
        }
      />
      {a.enabled && (
        <div className="mt-3 flex flex-col gap-2 text-sm">
          <Button variant="outline" className="self-start" disabled={test.isPending} onClick={() => test.mutate()}>
            {test.isPending ? "Testing…" : "Test the provider"}
          </Button>
          {r && (
            <p role="status" className={r.ok ? "" : "text-destructive"}>
              {r.ok
                ? `The provider answered in ${r.latencyMs} ms.`
                : `The provider failed (${r.code ?? "error"}): ${r.message ?? ""}`}
            </p>
          )}
          {test.error && <ProblemAlert error={test.error} />}
        </div>
      )}
    </Card>
  )
}

/** The admin page's sections, each at its own address (`/admin#visibility`); the overview is the page without one. */
export const sections = [
  { id: "overview", label: "Overview" },
  { id: "visibility", label: "Who sees what" },
  { id: "announcements", label: "Announcements" },
  { id: "github", label: "GitHub" },
  { id: "assistant", label: "Assistant" },
] as const

type Section = (typeof sections)[number]["id"]

const onHash = (changed: () => void) => {
  globalThis.addEventListener("hashchange", changed)
  return () => globalThis.removeEventListener("hashchange", changed)
}

/** useSection is the section the address names, the overview when it names none. */
function useSection(): Section {
  const hash = useSyncExternalStore(
    onHash,
    () => globalThis.location.hash.slice(1),
    () => "",
  )
  return sections.find((x) => x.id === hash)?.id ?? "overview"
}

/** SectionNav is the side menu: a column beside the section on a wide screen, a row that scrolls above it on a phone. */
function SectionNav({ current, attention }: { current: Section; attention: Partial<Record<Section, boolean>> }) {
  return (
    <nav aria-label="Admin sections" className="lg:sticky lg:top-20 lg:self-start">
      <ul className="-mx-4 flex gap-1 overflow-x-auto px-4 pb-1 lg:mx-0 lg:flex-col lg:px-0">
        {sections.map((x) => (
          <li key={x.id} className="shrink-0">
            <a
              href={x.id === "overview" ? "#" : `#${x.id}`}
              aria-current={x.id === current ? "page" : undefined}
              onClick={(e) => {
                if (x.id !== "overview") return
                // An empty hash adds no entry and fires no hashchange: set the address without one.
                e.preventDefault()
                globalThis.history.pushState(null, "", globalThis.location.pathname + globalThis.location.search)
                globalThis.dispatchEvent(new HashChangeEvent("hashchange"))
              }}
              className={cn(
                "flex items-center gap-2 rounded-md px-3 py-1.5 text-sm whitespace-nowrap hover:bg-muted",
                x.id === current && "bg-muted font-semibold",
              )}
            >
              {x.label}
              {attention[x.id] && <span className="size-2 rounded-full bg-amber-500" role="img" aria-label="needs action" />}
            </a>
          </li>
        ))}
      </ul>
    </nav>
  )
}

/** AdminView is the operator's page in sections: the overview of what needs action, who sees what, the announcements, GitHub and
 * the assistant. */
export function AdminView() {
  const { data: me } = useQuery(meQuery(api))
  // Only an administrator asks for the status; a session that ends meanwhile answers 401 or 403.
  const { data, error } = useQuery({ ...adminStatusQuery(get), retry: false, enabled: !!me?.admin })
  const { data: pre } = useQuery({ ...prerequisitesQuery(api), enabled: !!me?.admin })
  const section = useSection()
  const status = error instanceof ProblemError ? error.status : me && !me.admin ? (me.signedIn ? 403 : 401) : 0
  const refused = status === 401 || status === 403
  const attention: Partial<Record<Section, boolean>> = {
    overview: (data?.warnings.length ?? 0) > 0,
    github: (pre?.unmet.length ?? 0) > 0 || !!data?.sources.some((src) => src.freshness !== "fresh"),
  }
  return (
    <>
      <PageHeader title="Admin">
        {me?.signedIn && (
          <p className="text-sm text-muted-foreground">
            Signed in as {me.name} through {via[me.via] ?? me.via}
            {me.admin ? ", an administrator" : ""}.
          </p>
        )}
      </PageHeader>
      {refused ? (
        <Alert role="alert">
          <p>
            {status === 401
              ? "Sign in as an administrator to see the portal's status."
              : "Your account is not an administrator of this portal."}
          </p>
          {status === 401 && (
            <Link href="/signin?return_to=/admin" className={buttonClass("default", "mt-3")}>
              Sign in
            </Link>
          )}
        </Alert>
      ) : error ? (
        <ProblemAlert error={error} />
      ) : (
        data && (
          <div className="grid grid-cols-[minmax(0,1fr)] gap-4 lg:grid-cols-[12rem_minmax(0,1fr)]">
            <SectionNav current={section} attention={attention} />
            <div className="min-w-0">
              {section === "overview" && <Overview s={data} />}
              {section === "visibility" && <VisibilityCard />}
              {section === "announcements" && <AnnouncementsCard />}
              {section === "github" && <GitHub s={data} />}
              {section === "assistant" && <AssistantCard />}
            </div>
          </div>
        )
      )}
    </>
  )
}
