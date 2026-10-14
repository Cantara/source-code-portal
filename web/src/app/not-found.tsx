import type { Metadata } from "next"
import Link from "next/link"
import { PageHeader } from "@/shared/page-header"

export const metadata: Metadata = { title: "Page not found", robots: { index: false } }

export default function NotFound() {
  return (
    <section>
      <PageHeader title="Page not found">
        <p className="text-muted-foreground">There is no page, group or repository at this address.</p>
      </PageHeader>
      <p>
        <Link href="/" className="underline">
          Back to the dashboard
        </Link>
      </p>
    </section>
  )
}
