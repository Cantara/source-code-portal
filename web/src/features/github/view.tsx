"use client"

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useId, useState, type FormEvent } from "react"
import { Alert } from "@/components/ui/alert"
import { Button, buttonClass } from "@/components/ui/button"
import { Card, CardTitle } from "@/components/ui/card"
import { get, ProblemError, send } from "@/lib/api/client"
import { useNow } from "@/lib/clock"
import { PageHeader } from "@/shared/page-header"
import { ProblemAlert } from "@/shared/problem-alert"
import { RelativeTime } from "@/shared/relative-time"
import { endsSoon, tokensKey, tokensQuery, type Owner, type Token } from "./api"

const permission: Record<string, string> = {
  contents: "Contents",
  metadata: "Metadata",
  pull_requests: "Pull requests",
  issues: "Issues",
  statuses: "Commit statuses",
  checks: "Checks",
}

const refused: Record<string, string> = {
  unauthorized: "GitHub refused the token: it expired or was revoked, or the enterprise's network rules refuse this portal.",
  forbidden: "GitHub refused the token for this organisation: it may still wait for the organisation's approval.",
  not_found: "The token no longer reads this organisation.",
  sealed: "The portal can no longer open the token: link it again.",
}

/** GitHubView is the reader's GitHub access: per organisation, the steps to make a token, the token linked and what it reads. */
export function GitHubView() {
  const { data, error } = useQuery(tokensQuery(get))
  return (
    <>
      <PageHeader title="Your GitHub access">
        <p className="max-w-prose text-muted-foreground">
          This portal shows you what GitHub lets you read, and nothing else. It reads GitHub with a token of your own for each
          organisation: a fine-grained personal access token that can only read. Until you link one you see no repositories.
        </p>
      </PageHeader>
      {error && <ProblemAlert error={error} />}
      {data && (
        <div className="grid gap-4">
          {data.sources.flatMap((s) =>
            s.owners.map((o) => (
              <OwnerCard key={`${s.source}/${o.owner}`} source={s.source} owner={o} permissions={data.permissions} />
            )),
          )}
        </div>
      )}
    </>
  )
}

function OwnerCard({ source, owner, permissions }: { source: string; owner: Owner; permissions: string[] }) {
  const qc = useQueryClient()
  const id = useId()
  const [token, setToken] = useState("")
  const done = () => qc.invalidateQueries({ queryKey: tokensKey })
  const link = useMutation({
    mutationFn: () => send<Token>("POST", "/me/github/tokens", { source, owner: owner.owner, token }),
    onSuccess: async () => {
      setToken("")
      await done()
    },
  })
  const unlink = useMutation({
    mutationFn: () =>
      send<undefined>("DELETE", `/me/github/tokens/${encodeURIComponent(source)}/${encodeURIComponent(owner.owner)}`),
    onSuccess: done,
  })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (token.trim() !== "") link.mutate()
  }
  const t = owner.token
  return (
    <Card>
      <CardTitle>{owner.owner}</CardTitle>
      {t ? (
        <Linked t={t} />
      ) : (
        <p className="mt-2 text-sm text-muted-foreground">No token linked: you see nothing of {owner.owner}.</p>
      )}
      <ol className="mt-4 grid max-w-prose list-decimal gap-3 pl-5 text-sm">
        <li>
          <a href={owner.createUrl} target="_blank" rel="noreferrer" className="font-medium underline underline-offset-2">
            Open GitHub&apos;s form for a new token
          </a>{" "}
          with its name, {owner.owner} as resource owner, 90 days and these permissions, all read-only, filled in:{" "}
          {permissions.map((p) => permission[p] ?? p).join(", ")}.
        </li>
        <li>
          Choose <strong>All repositories</strong>, then <strong>Generate token</strong>. If your organisation approves tokens,
          wait for its approval.
        </li>
        <li>
          <form onSubmit={submit} className="flex flex-wrap items-end gap-2">
            <label htmlFor={id} className="grid gap-1">
              <span>Paste the token here{t ? " to replace the one linked" : ""}</span>
              <input
                id={id}
                type="password"
                autoComplete="off"
                spellCheck={false}
                value={token}
                onChange={(e) => setToken(e.target.value)}
                placeholder="github_pat_…"
                className="min-h-9 w-80 max-w-full rounded-md border border-input bg-background px-3 font-mono text-sm"
              />
            </label>
            <Button type="submit" disabled={link.isPending || token.trim() === ""}>
              {link.isPending ? "Checking with GitHub…" : "Link"}
            </Button>
          </form>
        </li>
      </ol>
      {link.error && <Refusal error={link.error} />}
      {t && (
        <div className="mt-4">
          <Button variant="outline" onClick={() => unlink.mutate()} disabled={unlink.isPending}>
            Remove the token
          </Button>
          {unlink.error && <ProblemAlert error={unlink.error} />}
        </div>
      )}
      <p className="mt-4 text-xs text-muted-foreground">
        The portal keeps the token encrypted, never shows it again and only reads with it. Revoke it on GitHub at any time:{" "}
        <a
          href={owner.createUrl.replace(/\/new\?.*$/, "")}
          target="_blank"
          rel="noreferrer"
          className={buttonClass("ghost", "h-auto min-h-0 px-1")}
        >
          your tokens
        </a>
        .
      </p>
    </Card>
  )
}

function Linked({ t }: { t: Token }) {
  const now = useNow()
  return (
    <div className="mt-2 grid gap-2 text-sm">
      <p>
        Linked as <strong>{t.login}</strong>
        {t.expiresAt && (
          <>
            , ends <RelativeTime at={t.expiresAt} />
          </>
        )}
        .{" "}
        {t.state === "ok" &&
          (t.listedAt ? (
            <>
              Your access was read <RelativeTime at={t.listedAt} />.
            </>
          ) : (
            "Your access is being read."
          ))}
      </p>
      {t.state !== "ok" && (
        <Alert variant="error">{refused[t.state] ?? `GitHub refused the token (${t.state}).`} Make a new one.</Alert>
      )}
      {t.state === "ok" && endsSoon(t, now) && (
        <Alert variant="warn">The token ends soon: make a new one and link it here.</Alert>
      )}
    </div>
  )
}

function Refusal({ error }: { error: unknown }) {
  if (error instanceof ProblemError && error.status === 422) {
    return (
      <Alert variant="error" className="mt-3" role="alert">
        {error.message}
      </Alert>
    )
  }
  return <ProblemAlert error={error} />
}
