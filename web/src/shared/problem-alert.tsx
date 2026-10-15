import { Alert } from "@/components/ui/alert"
import { ProblemError } from "@/lib/api/client"

const titles: Record<string, string> = {
  "not-found": "Not found",
  "not-ready": "The portal is still loading",
  "queue-full": "The portal is busy",
  internal: "Something went wrong on the server",
  network: "The portal could not be reached",
  unauthenticated: "Sign in first",
  forbidden: "Not allowed",
}

/** problemTitle maps a problem code to its title. */
export function problemTitle(code: string): string {
  return titles[code] ?? "Unexpected problem"
}

export function ProblemAlert({ error }: { error: unknown }) {
  const p = error instanceof ProblemError ? error : new ProblemError(0, "unexpected", error instanceof Error ? error.message : "")
  return (
    <Alert variant="error" role="alert">
      <p className="font-semibold">{problemTitle(p.code)}</p>
      {p.message && <p>{p.message}</p>}
      <p className="mt-1 text-xs">Request id: {p.requestId || "none"}</p>
    </Alert>
  )
}
