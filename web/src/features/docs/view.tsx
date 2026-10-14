"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { Alert } from "@/components/ui/alert"
import { api } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { docAddress, withoutLeadingH1 } from "@/lib/docs"
import { docPath, repoPath } from "@/lib/paths"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { PageHeader } from "@/shared/page-header"
import { PanelBoundary } from "@/shared/panel-boundary"
import { ProblemAlert } from "@/shared/problem-alert"
import { SafeHtml } from "@/shared/safe-html"
import { docListQuery, docQuery } from "./api"

/** DocView is one document of a repository with the repository's document list and the document's outline. */
export function DocView({ owner, name, path }: { owner: string; name: string; path: string }) {
  const { data, error } = useQuery(docQuery(api, owner, name, path))
  return (
    <>
      <PageHeader title={data?.title || path} meta={data && <FreshnessBadge freshness={data.freshness} />}>
        <p className="flex flex-wrap gap-x-3 text-sm">
          <Link href={repoPath(owner, name)} className="hover:underline">
            {owner}/{name}
          </Link>
          {data && (
            <a href={data.sourceUrl} className="hover:underline" rel="noreferrer">
              {data.path} on GitHub
            </a>
          )}
        </p>
      </PageHeader>
      <div className="grid gap-6 lg:grid-cols-[14rem_1fr_14rem]">
        <nav aria-label="Documents" className="text-sm">
          <PanelBoundary>
            <Files owner={owner} name={name} current={path} />
          </PanelBoundary>
        </nav>
        <div className="min-w-0">
          {error && !data && <ProblemAlert error={error} />}
          {data &&
            (data.state === "ok" ? (
              <article aria-label={data.title || data.path}>
                <SafeHtml html={withoutLeadingH1(data.html)} className="doc" />
                {data.truncated && (
                  <p className="mt-4 text-sm text-muted-foreground">Only the first part of this document is shown.</p>
                )}
              </article>
            ) : (
              <Alert>
                This document is too large or not in a supported format.{" "}
                <a href={data.sourceUrl} className="underline">
                  Read it on GitHub
                </a>
              </Alert>
            ))}
        </div>
        {data && data.toc.length > 0 && (
          <nav aria-label="On this page" className="text-sm">
            <h2 className="mb-2 font-semibold">On this page</h2>
            <ul>
              {data.toc.map((h) => (
                <li key={h.id} className={cn(h.level > 2 && "pl-3", h.level > 3 && "pl-6")}>
                  <a href={`#${h.id}`} className="inline-block min-h-6 py-0.5 hover:underline">
                    {h.text}
                  </a>
                </li>
              ))}
            </ul>
          </nav>
        )}
      </div>
    </>
  )
}

function Files({ owner, name, current }: { owner: string; name: string; current: string }) {
  const { data, error } = useQuery(docListQuery(api, owner, name))
  if (error && !data) return <ProblemAlert error={error} />
  if (!data) return null
  return (
    <ul className="flex flex-col gap-1">
      {data.docs.map((d) => (
        <li key={d.id}>
          {docAddress(d.path) === docAddress(current) ? (
            <span aria-current="page" className="font-semibold">
              {d.title || d.path}
            </span>
          ) : (
            <Link href={docPath(owner, name, d.path)} className="hover:underline">
              {d.title || d.path}
            </Link>
          )}
        </li>
      ))}
    </ul>
  )
}
