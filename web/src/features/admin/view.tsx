"use client"

import { useMutation, useQuery } from "@tanstack/react-query"
import Link from "next/link"
import type { ReactNode } from "react"
import { Alert } from "@/components/ui/alert"
import { Button, buttonClass } from "@/components/ui/button"
import { Card, CardTitle } from "@/components/ui/card"
import { api, get, ProblemError } from "@/lib/api/client"
import { assistantQuery, meQuery } from "@/lib/api/queries"
import { plural } from "@/lib/format"
import { PageHeader } from "@/shared/page-header"
import { ProblemAlert } from "@/shared/problem-alert"
import { RelativeTime } from "@/shared/relative-time"
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

/** Status draws the operator's status. */
export function Status({ s }: { s: AdminStatus }) {
  return (
    <div className="grid gap-4 lg:grid-cols-2">
      {s.warnings.length > 0 && (
        <Alert variant="warn" className="lg:col-span-2">
          <p className="font-semibold">{plural(s.warnings.length, "warning", "warnings")}</p>
          <ul className="mt-1 list-disc pl-5">
            {s.warnings.map((w) => (
              <li key={w}>{w}</li>
            ))}
          </ul>
        </Alert>
      )}
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
          ]}
        />
      </Card>
      <Card>
        <CardTitle>Sources</CardTitle>
        <table className="mt-3 w-full text-sm">
          <thead>
            <tr className="text-left text-muted-foreground">
              <th scope="col" className="py-1 font-normal">
                Source
              </th>
              <th scope="col" className="py-1 font-normal">
                Repositories
              </th>
              <th scope="col" className="py-1 font-normal">
                Listing
              </th>
              <th scope="col" className="py-1 font-normal">
                Rate limit left
              </th>
            </tr>
          </thead>
          <tbody>
            {s.sources.map((src) => (
              <tr key={src.id} className="border-t border-border">
                <td className="py-1 font-mono">{src.id}</td>
                <td className="py-1">{src.repos}</td>
                <td className="py-1">
                  {src.freshness}, <RelativeTime at={src.checkedAt} />
                </td>
                <td className="py-1">{Math.round(src.rateRemaining * 100)}%</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>
      <Card>
        <CardTitle>Webhook</CardTitle>
        <Facts
          rows={[
            ["Configured", yes(s.webhook.enabled)],
            ["Last delivery", <RelativeTime key="l" at={s.webhook.lastDelivery} />],
            ...Object.entries(s.webhook.deliveries)
              .sort(([a], [b]) => a.localeCompare(b))
              .map(([result, n]): [string, ReactNode] => [`Deliveries ${result}`, n]),
          ]}
        />
      </Card>
    </div>
  )
}

const access: Record<string, string> = { "signed-in": "signed-in viewers", admin: "administrators", everyone: "everyone" }

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

/** AdminView is the operator's page: who is signed in and the portal's status. */
export function AdminView() {
  const { data: me } = useQuery(meQuery(api))
  // Only an administrator asks for the status; a session that ends meanwhile answers 401 or 403.
  const { data, error } = useQuery({ ...adminStatusQuery(get), retry: false, enabled: !!me?.admin })
  const status = error instanceof ProblemError ? error.status : me && !me.admin ? (me.signedIn ? 403 : 401) : 0
  const refused = status === 401 || status === 403
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
          <>
            <Status s={data} />
            <div className="mt-4 grid gap-4 lg:grid-cols-2">
              <AssistantCard />
            </div>
          </>
        )
      )}
    </>
  )
}
