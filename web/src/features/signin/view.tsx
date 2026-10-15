"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { Alert } from "@/components/ui/alert"
import { Button, buttonClass } from "@/components/ui/button"
import { Card, CardTitle } from "@/components/ui/card"
import { api } from "@/lib/api/client"
import { meQuery } from "@/lib/api/queries"
import { PageHeader } from "@/shared/page-header"
import { ProblemAlert } from "@/shared/problem-alert"

/** errors explain the codes the sign-in routes send the browser back with. */
const errors: Record<string, string> = {
  provider: "The sign-in provider could not be reached or did not confirm the sign-in. Try again in a moment.",
  expired: "The sign-in took too long, or was started in another browser. Start it again.",
  denied: "The sign-in was cancelled at the provider.",
  state: "The sign-in could not be verified. Start it again.",
  token: "That token is not valid.",
}

/** safeReturn keeps a path of the portal, else the dashboard (the server checks it again). */
export function safeReturn(to: string | undefined): string {
  return to && to.startsWith("/") && !to.startsWith("//") && !to.includes("\\") && !to.startsWith("/auth/") ? to : "/"
}

/** SignInView offers the ways to sign in this portal has: the organisation's provider, an access token. */
export function SignInView({ error, returnTo }: { error?: string; returnTo?: string }) {
  const to = safeReturn(returnTo)
  const { data: me, error: failed } = useQuery(meQuery(api))
  return (
    <div className="mx-auto max-w-md">
      <PageHeader title="Sign in" />
      {error && (
        <Alert variant="error" role="alert" className="mb-4">
          {errors[error] ?? "The sign-in did not succeed. Start it again."}
        </Alert>
      )}
      {failed && <ProblemAlert error={failed} />}
      {me?.signedIn ? (
        <Card>
          <p>
            You are signed in as <strong>{me.name}</strong>.
          </p>
          <Link href={to} className={buttonClass("default", "mt-3")}>
            Continue
          </Link>
        </Card>
      ) : (
        me && (
          <div className="flex flex-col gap-4">
            <p className="text-sm text-muted-foreground">Signing in shows the repositories your groups may see.</p>
            {me.signIn.oidc && (
              <Card>
                <CardTitle>Your organisation&apos;s account</CardTitle>
                {/* A plain link: /auth/login is the Go server's, which sends the browser on to the provider. */}
                <a href={`/auth/login?return_to=${encodeURIComponent(to)}`} className={buttonClass("default", "mt-3 w-full")}>
                  Sign in with single sign-on
                </a>
              </Card>
            )}
            {me.signIn.token && (
              <Card>
                <CardTitle>Access token</CardTitle>
                <form method="post" action="/auth/token" className="mt-3 flex flex-col gap-3">
                  <input type="hidden" name="return_to" value={to} />
                  <label className="flex flex-col gap-1 text-sm">
                    Token
                    <input
                      type="password"
                      name="token"
                      required
                      autoComplete="off"
                      spellCheck={false}
                      className="min-h-9 rounded-md border border-input bg-background px-2 font-mono"
                    />
                  </label>
                  <Button type="submit" variant="outline">
                    Sign in with the token
                  </Button>
                </form>
              </Card>
            )}
            {!me.signIn.oidc && !me.signIn.token && <p>This portal has no sign-in.</p>}
          </div>
        )
      )}
    </div>
  )
}
