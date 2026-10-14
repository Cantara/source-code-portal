import { useSuspenseQuery } from "@tanstack/react-query"
import { createFileRoute } from "@tanstack/react-router"
import { pageQuery } from "./api"
import { SafeHtml } from "@/shared/safe-html"

function PageView() {
  const { slug } = Route.useParams()
  const { data } = useSuspenseQuery(pageQuery(slug))
  return (
    <article>
      <title>{data.title}</title>
      <h1 className="text-2xl font-semibold">{data.title}</h1>
      <SafeHtml html={data.html} className="prose max-w-none dark:prose-invert" />
    </article>
  )
}

export const Route = createFileRoute("/pages/$slug")({
  loader: ({ context, params }) => context.queryClient.ensureQueryData({ ...pageQuery(params.slug), retry: false }),
  component: PageView,
})
