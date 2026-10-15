"use client"

import { useQuery } from "@tanstack/react-query"
import type { ReactNode } from "react"
import Link from "next/link"
import { Alert } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Card, CardTitle } from "@/components/ui/card"
import { api, type Schemas } from "@/lib/api/client"
import { withoutLeadingH1 } from "@/lib/docs"
import { docPath } from "@/lib/paths"
import { EmptyState } from "@/shared/empty-state"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { crumbs } from "@/lib/crumbs"
import { PageHeader } from "@/shared/page-header"
import { PanelBoundary } from "@/shared/panel-boundary"
import { ProblemAlert } from "@/shared/problem-alert"
import { RelativeTime } from "@/shared/relative-time"
import { SafeHtml } from "@/shared/safe-html"
import { VisibilityBadge } from "@/entities/repo/visibility-badge"
import { docListQuery, docQuery, repoQuery } from "@/lib/api/queries"
import { RepoBadges } from "./badges"
import { readmeOf } from "./readme"

/**
 * RepoView is a repository: descriptor, badges, README and documents, with the panels other features contribute
 * (composed in app/) in the main and side columns.
 */
export function RepoView({ owner, name, main, side }: { owner: string; name: string; main?: ReactNode; side?: ReactNode }) {
  const { data, error } = useQuery(repoQuery(api, owner, name))
  const repo = data?.repo
  return (
    <>
      <PageHeader
        title={data?.title || `${owner}/${name}`}
        meta={repo && <FreshnessBadge freshness={repo.freshness} />}
        trail={crumbs.repo(repo?.owner ?? owner, repo?.name ?? name, data?.group)}
      >
        {repo && (
          <>
            {(data.descriptor.description || repo.description) && (
              <p className="text-muted-foreground">{data.descriptor.description || repo.description}</p>
            )}
            <p className="flex flex-wrap gap-x-3 text-sm">
              <a href={repo.htmlUrl} className="hover:underline" rel="noreferrer">
                {repo.owner}/{repo.name} on GitHub
              </a>
              {repo.homepage && (
                <a href={repo.homepage} className="hover:underline" rel="noreferrer nofollow">
                  Homepage
                </a>
              )}
              <span className="text-muted-foreground">
                Last push <RelativeTime at={repo.pushedAt} />
              </span>
              <VisibilityBadge visibility={repo.visibility} />
              {repo.archived && <Badge variant="warn">Archived</Badge>}
            </p>
            <RepoBadges address={repo.address} />
          </>
        )}
      </PageHeader>
      {error && !data && <ProblemAlert error={error} />}
      {data && (
        <div className="grid gap-6 lg:grid-cols-3">
          <div className="flex min-w-0 flex-col gap-6 lg:col-span-2">
            <PanelBoundary>
              <Readme owner={owner} name={name} />
            </PanelBoundary>
            {main}
          </div>
          <div className="flex min-w-0 flex-col gap-6">
            {data.descriptor.present && <DescriptorCard d={data.descriptor} />}
            {side}
            <PanelBoundary>
              <DocsCard owner={owner} name={name} />
            </PanelBoundary>
          </div>
        </div>
      )}
    </>
  )
}

function DescriptorCard({ d }: { d: Schemas["Descriptor"] }) {
  return (
    <Card aria-labelledby="about">
      <CardTitle id="about">About</CardTitle>
      <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
        {d.lifecycle && (
          <>
            <dt className="text-muted-foreground">Lifecycle</dt>
            <dd>
              <Badge variant={d.lifecycle === "deprecated" ? "warn" : "default"}>{d.lifecycle}</Badge>
            </dd>
          </>
        )}
        {d.owners.length > 0 && (
          <>
            <dt className="text-muted-foreground">Owners</dt>
            <dd>{d.owners.join(", ")}</dd>
          </>
        )}
        {d.tags.length > 0 && (
          <>
            <dt className="text-muted-foreground">Tags</dt>
            <dd className="flex flex-wrap gap-1">
              {d.tags.map((t) => (
                <Badge key={t}>{t}</Badge>
              ))}
            </dd>
          </>
        )}
      </dl>
      {d.links.length > 0 && (
        <ul className="mt-2 text-sm">
          {d.links.map((l) => (
            <li key={l.url}>
              <a href={l.url} className="hover:underline" rel="noreferrer nofollow">
                {l.title}
              </a>
            </li>
          ))}
        </ul>
      )}
      {d.problems.length > 0 && (
        <Alert variant="warn" className="mt-2">
          <p className="font-semibold">{d.file} has problems</p>
          <ul className="list-disc pl-5">
            {d.problems.map((p) => (
              <li key={p.pointer + p.code}>
                {p.pointer && <code>{p.pointer}</code>} {p.message}
              </li>
            ))}
          </ul>
        </Alert>
      )}
    </Card>
  )
}

function DocsCard({ owner, name }: { owner: string; name: string }) {
  const { data, error } = useQuery(docListQuery(api, owner, name))
  const docs = data?.docs.filter((d) => !d.readme) ?? []
  return (
    <Card aria-labelledby="docs">
      <CardTitle id="docs">Documentation</CardTitle>
      {error && !data && <ProblemAlert error={error} />}
      {data &&
        (docs.length === 0 ? (
          <p className="text-sm text-muted-foreground">No documents under docs/.</p>
        ) : (
          <ul className="mt-2 text-sm">
            {docs.map((d) => (
              <li key={d.id}>
                <Link href={docPath(owner, name, d.path)} className="hover:underline">
                  {d.title || d.path}
                </Link>
              </li>
            ))}
          </ul>
        ))}
    </Card>
  )
}

function Readme({ owner, name }: { owner: string; name: string }) {
  const list = useQuery(docListQuery(api, owner, name))
  const readme = readmeOf(list.data)
  const doc = useQuery({ ...docQuery(api, owner, name, readme?.id ?? ""), enabled: readme !== undefined })
  if (list.error && !list.data) return <ProblemAlert error={list.error} />
  if (list.data && !readme) return <EmptyState title="This repository has no README" />
  if (doc.error && !doc.data) return <ProblemAlert error={doc.error} />
  if (!doc.data) return <p className="text-muted-foreground">Loading the README…</p>
  return (
    <article aria-label="README" className="rounded-lg border border-border p-4">
      {doc.data.state === "ok" ? (
        <SafeHtml html={withoutLeadingH1(doc.data.html)} className="doc" />
      ) : (
        <p>
          The README is too large or not in a supported format.{" "}
          <a href={doc.data.sourceUrl} className="underline">
            Read it on GitHub
          </a>
        </p>
      )}
      {doc.data.truncated && <p className="mt-4 text-sm text-muted-foreground">Only the first part is shown.</p>}
    </article>
  )
}
