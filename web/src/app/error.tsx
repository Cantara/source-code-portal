"use client"

import { Button } from "@/components/ui/button"
import { PageHeader } from "@/shared/page-header"
import { ProblemAlert } from "@/shared/problem-alert"

/** Error catches what no panel boundary did; Try again renders the page anew. */
export default function Error({ error, reset }: { error: Error; reset: () => void }) {
  return (
    <section>
      <PageHeader title="Something went wrong" />
      <ProblemAlert error={error} />
      <Button variant="outline" className="mt-3" onClick={reset}>
        Try again
      </Button>
    </section>
  )
}
