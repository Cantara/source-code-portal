import { Link } from "@tanstack/react-router"
import { PageHeader } from "@/shared/page-header"

export function NotFound() {
  return (
    <section>
      <PageHeader title="Page not found">
        <p className="text-muted-foreground">There is no page, group or repository at this address.</p>
      </PageHeader>
      <p>
        <Link to="/" className="underline">
          Back to the dashboard
        </Link>
      </p>
    </section>
  )
}
