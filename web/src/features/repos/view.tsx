"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { Alert } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Card, CardTitle } from "@/components/ui/card"
import { api, type Schemas } from "@/lib/api/client"
import { withoutLeadingH1 } from "@/lib/docs"
import { docPath, repoCommitsPath } from "@/lib/paths"
import { CommitList } from "@/shared/commit-list"
import { EmptyState } from "@/shared/empty-state"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { PageHeader } from "@/shared/page-header"
import { PanelBoundary } from "@/shared/panel-boundary"
import { ProblemAlert } from "@/shared/problem-alert"
import { RelativeTime } from "@/shared/relative-time"
import { SafeHtml } from "@/shared/safe-html"
import { RepoBadges } from "@/shared/repo-badges"
import { VisibilityBadge } from "@/shared/visibility-badge"
import { docListQuery, docQuery, repoQuery } from "./api"
import { Dependencies, TechnologyCard } from "./manifests"

/** RepoView is a repository: descriptor, badges, README, technology, dependencies, documents and the latest commits. */
export function RepoView({ owner, name }: { owner: string; name: string }) {
  const { data, error } = useQuery(repoQuery(api, owner, name))
  const repo = data?.repo
  return (
    <>
      <PageHeader title={data?.title || `${owner}/${name}`} meta={repo && <FreshnessBadge freshness={repo.freshness} />}>
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
            <PanelBoundary>
              <Dependencies owner={owner} name={name} />
            </PanelBoundary>
          </div>
          <div className="flex min-w-0 flex-col gap-6">
            {data.descriptor.present && <DescriptorCard d={data.descriptor} />}
            <PanelBoundary>
              <TechnologyCard owner={owner} name={name} />
            </PanelBoundary>
            <PanelBoundary>
              <DocsCard owner={owner} name={name} />
            </PanelBoundary>
            <Card aria-labelledby="latest">
              <div className="flex flex-wrap items-baseline justify-between gap-2">
                <CardTitle id="latest">Latest commits</CardTitle>
                <FreshnessBadge freshness={data.commitsFreshness} />
              </div>
              <CommitList commits={data.commits} showRepo={false} />
              <Link href={repoCommitsPath(owner, name)} className="mt-2 inline-block text-sm hover:underline">
                Commit log
              </Link>
            </Card>
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
  const readme = list.data?.docs.find((d) => d.readme)
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
