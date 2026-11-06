import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import type { ReactNode } from "react"
import { AllCommitsView } from "@/features/commits/view"
import { DashboardView } from "@/features/dashboard/view"
import { DocView } from "@/features/docs/view"
import { GroupView } from "@/features/groups/view"
import { IssuesView } from "@/features/issues/view"
import { AlumniView } from "@/features/alumni/view"
import { PullsView } from "@/features/pulls/view"
import { InsightsView } from "@/features/insights/view"
import { ReleasesView } from "@/features/releases/view"
import { repoRoute } from "@/features/repos/route"
import { RepoView } from "@/features/repos/view"
import type { Ctx } from "@/lib/page"
import { panels } from "./repos/[owner]/[name]/panels"
import { StatusBanners } from "@/features/status/banners"
import { setLocation } from "@/test/navigation"
import { MainNav } from "./shell"
import { RulesProvider } from "@/lib/rules-context"
import { activity, commit, fresh, http, noPulls, rules, server, snapshot } from "@/test/server"

function client() {
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 10_000 } } })
}

/** show renders a view at path inside the shell's providers and banners, as a page does after hydration. */
function show(view: ReactNode, path = "/", qc = client()) {
  setLocation(path)
  return render(
    <QueryClientProvider client={qc}>
      <StatusBanners />
      {view}
    </QueryClientProvider>,
  )
}

describe("pages", () => {
  it("lists the open pull requests whose turn first, the states as filters", async () => {
    show(<PullsView defaultSize={50} />, "/pulls")
    expect(await screen.findByRole("heading", { level: 1, name: "Pull requests" })).toBeInTheDocument()
    const chips = within(await screen.findByRole("navigation", { name: "By whose turn" }))
    expect(chips.getByRole("link", { name: /All open 2/ })).toHaveAttribute("aria-current", "page")
    expect(chips.getByRole("link", { name: /Needs review 1/ })).toHaveAttribute("href", "/pulls?state=review")
    expect(chips.getByRole("link", { name: /Ready to merge 1/ })).toBeInTheDocument()
    expect(chips.queryByRole("link", { name: /Failing checks/ })).toBeNull()
    expect(screen.getByRole("link", { name: "Change 1" })).toHaveAttribute("href", "https://github.com/acme/core/pull/1")
    expect(screen.getByRole("form", { name: "Filter pull requests" })).toHaveAttribute("action", "/pulls")
  })

  it("answers five questions about the fleet, naming repositories and no one", async () => {
    show(<InsightsView />, "/insights")
    expect(await screen.findByRole("heading", { level: 1, name: "Insights" })).toBeInTheDocument()
    expect(await screen.findByText("3 stable releases in the last 12 months.")).toBeInTheDocument()
    for (const q of [
      "How often does the fleet ship?",
      "What is the fleet built on?",
      "What does the fleet lean on?",
      "Where does knowledge sit with one person?",
      "What has gone quiet?",
    ])
      expect(screen.getByRole("heading", { name: q })).toBeInTheDocument()
    expect(screen.getByText("Go in 3 repositories, 60% of the fleet.")).toBeInTheDocument()
    expect(screen.getByRole("img", { name: /Stable releases per month/ })).toBeInTheDocument()
    expect(screen.getByRole("cell", { name: "None released" })).toBeInTheDocument()
    expect(screen.getByText(/Archived, still used/)).toBeInTheDocument()
    const single = within(screen.getByRole("list", { name: "Repositories mostly one person's" }))
    expect(single.getByRole("link", { name: "acme/api" })).toHaveAttribute("href", "/repos/acme/api")
    expect(
      within(screen.getByRole("list", { name: "Dormant repositories" })).getByRole("link", { name: "acme/quiet" }),
    ).toBeInTheDocument()
  })

  it("lists the recent releases, the variants as filters with their counts", async () => {
    show(<ReleasesView defaultSize={50} />, "/releases")
    expect(await screen.findByRole("heading", { level: 1, name: "Releases" })).toBeInTheDocument()
    const chips = within(await screen.findByRole("navigation", { name: "By variant" }))
    expect(chips.getByRole("link", { name: /Latest per repository 2/ })).toHaveAttribute("aria-current", "true")
    expect(chips.getByRole("link", { name: /Every release 3/ })).toHaveAttribute("href", "/releases?variant=all")
    expect(chips.getByRole("link", { name: /Pre-releases 1/ })).toHaveAttribute("href", "/releases?variant=pre")
    const rows = within(screen.getByRole("list", { name: "Release list" })).getAllByRole("listitem")
    expect(rows).toHaveLength(2)
    expect(rows[0]).toHaveTextContent("acme/core")
    expect(rows[0]).toHaveTextContent("Pre-release")
    expect(rows[0]).toHaveTextContent("Faster login")
    expect(screen.getByRole("link", { name: "v2.1.0-rc1" })).toHaveAttribute(
      "href",
      "https://github.com/acme/core/releases/tag/v2.1.0-rc1",
    )
    expect(rows[1]).toHaveTextContent("(automation)")
    expect(rows[1]).not.toHaveTextContent("Pre-release")
    expect(screen.getByRole("form", { name: "Filter the releases" })).toHaveAttribute("action", "/releases")
  })

  it("lists the retired repositories with why, when they were last active and what still depends on them", async () => {
    show(<AlumniView defaultSize={50} />, "/alumni")
    expect(await screen.findByRole("heading", { level: 1, name: "Alumni" })).toBeInTheDocument()
    const chips = within(await screen.findByRole("navigation", { name: "By reason" }))
    expect(chips.getByRole("link", { name: /All 2/ })).toHaveAttribute("aria-current", "true")
    expect(chips.getByRole("link", { name: /Archived 1/ })).toHaveAttribute("href", "/alumni?reason=archived")
    expect(chips.getByRole("link", { name: /Deprecated 1/ })).toHaveAttribute("href", "/alumni?reason=deprecated")
    const rows = within(screen.getByRole("list", { name: "Alumni" })).getAllByRole("listitem")
    expect(rows).toHaveLength(2)
    expect(rows[0]).toHaveTextContent("Archived")
    expect(rows[0]).toHaveTextContent("used by 2")
    expect(rows[0]).toHaveTextContent("3 authors")
    expect(rows[1]).toHaveTextContent("Deprecated")
    expect(rows[1]).not.toHaveTextContent("used by")
    expect(screen.getByRole("form", { name: "Filter the alumni" })).toHaveAttribute("action", "/alumni")
  })

  it("lists the open issues what waits first, the states as filters", async () => {
    show(<IssuesView defaultSize={50} />, "/issues")
    expect(await screen.findByRole("heading", { level: 1, name: "Issues" })).toBeInTheDocument()
    const chips = within(await screen.findByRole("navigation", { name: "By where they stand" }))
    expect(chips.getByRole("link", { name: /All open 2/ })).toHaveAttribute("aria-current", "page")
    expect(chips.getByRole("link", { name: /Needs triage 1/ })).toHaveAttribute("href", "/issues?state=triage")
    expect(chips.queryByRole("link", { name: /Unassigned/ })).toBeNull()
    const list = within(screen.getByRole("list", { name: "Issues" }))
    expect(list.getByRole("link", { name: "Problem 1" })).toHaveAttribute("href", "https://github.com/acme/core/issues/1")
    expect(list.getByText("for bo")).toBeInTheDocument()
    expect(list.getByText("Bug")).toBeInTheDocument()
    expect(screen.getByRole("form", { name: "Filter issues" })).toHaveAttribute("action", "/issues")
  })

  it("warns on /issues when the GitHub App lacks what issues need; an administrator gets the fix", async () => {
    server.use(
      http.get("/prerequisites", ({ response }) =>
        response(200).json({ unmet: [{ feature: "issues", title: "Issues", effect: "Open issues are not read." }] }),
      ),
    )
    show(<IssuesView defaultSize={50} />, "/issues")
    const notice = await screen.findByRole("status", { name: "Disabled feature" })
    expect(notice).toHaveTextContent("Open issues are not read.")
    expect(notice).toHaveTextContent("An administrator of the portal can grant what it needs on GitHub.")
    expect(within(notice).queryByRole("link")).toBeNull()
  })

  it("tells an administrator what to grant", async () => {
    server.use(
      http.get("/prerequisites", ({ response }) =>
        response(200).json({
          unmet: [
            {
              feature: "pulls",
              title: "Pull requests",
              effect: "Open pull requests are not read.",
              source: "gh",
              owners: ["acme"],
              permissions: ["Pull requests", "Checks"],
              events: [],
              refused: false,
              fix: "In the GitHub App's settings, Permissions & events: set Pull requests and Checks to Read-only.",
            },
          ],
        }),
      ),
    )
    show(<PullsView defaultSize={50} />, "/pulls")
    const notice = await screen.findByRole("status", { name: "Disabled feature" })
    expect(notice).toHaveTextContent("Missing read permissions: Pull requests, Checks.")
    expect(notice).toHaveTextContent("Installation: acme (source gh).")
    expect(within(notice).getByRole("link", { name: "What the portal needs from GitHub" })).toHaveAttribute(
      "href",
      expect.stringContaining("operator-guide.md#1-connect-github"),
    )
  })

  it("shows no warning when every prerequisite holds", async () => {
    show(<IssuesView defaultSize={50} />, "/issues")
    expect(await screen.findByRole("heading", { level: 1, name: "Issues" })).toBeInTheDocument()
    expect(screen.queryByRole("status", { name: "Disabled feature" })).toBeNull()
  })

  it("renders the dashboard with the fleet, the products' signals and the team's latest commits", async () => {
    show(<DashboardView work={["hand", "ai"]} />)
    expect(await screen.findByRole("heading", { level: 1, name: "Dashboard" })).toBeInTheDocument()
    expect(await screen.findByRole("link", { name: "IAM" })).toHaveAttribute("href", "/groups/iam")
    expect(screen.getByText("Commit 1")).toBeInTheDocument()
    expect(screen.getByRole("heading", { name: "Latest commits" })).toBeInTheDocument()
    // The card says the team's work, by hand and with AI, apart from automation's, who did it, and what most members share once.
    const card = screen.getByRole("link", { name: "IAM" }).closest("li") as HTMLElement
    expect(card).toHaveTextContent("4 commits")
    expect(card).toHaveTextContent("1 by hand · 3 with AI (Claude)")
    expect(card).toHaveTextContent("9 by automation")
    expect(
      within(card).getByRole("img", {
        name: /by hand 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 3; with AI 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 2/,
      }),
    ).toBeInTheDocument()
    expect(card).toHaveTextContent("ada and Bo")
    expect(within(card).getByText("ada")).toHaveAttribute("title", "Ada Lovelace")
    expect(card).toHaveTextContent("Apache-2.0 · Java · 1 of 2 released")
    expect(within(card).getByRole("link", { name: "whydah-core" })).toHaveAttribute("href", "/repos/acme/whydah-core")
    // The fleet map counts every member by its last change by a person.
    expect(screen.getByRole("region", { name: "Fleet" })).toHaveTextContent("2 active")
    // The telemetry counts commits of each kind, in the window the server's rules give, and says so.
    const telemetry = within(screen.getByRole("region", { name: "Telemetry" }))
    expect(telemetry.getByText("Commits by hand").nextElementSibling).toHaveTextContent("1")
    expect(telemetry.getByText("in the last 30 days, by 2 people")).toBeInTheDocument()
    expect(telemetry.getByText("Commits with AI").nextElementSibling).toHaveTextContent("3")
    expect(telemetry.getByText("in the last 30 days, with AI or by an agent (Claude, Copilot)")).toBeInTheDocument()
    expect(telemetry.getByText("Active repositories").nextElementSibling).toHaveTextContent("2")
    // The latest commits mark the one made with an AI, naming it.
    expect(screen.getByText("with Claude Opus 5")).toBeInTheDocument()
  })

  it("explains the fleet map's legend: the ranges always, the rest on request", async () => {
    const user = userEvent.setup()
    show(<DashboardView />)
    const fleet = within(await screen.findByRole("region", { name: "Fleet" }))
    expect(fleet.getByRole("list", { name: "Activity of every repository" })).toHaveTextContent("Active≤30d")
    const about = fleet.getByRole("button", { name: "About the fleet map" })
    expect(about).toHaveAttribute("aria-expanded", "false")
    expect(fleet.queryByRole("note")).not.toBeInTheDocument()
    await user.click(about)
    expect(fleet.getByRole("note", { name: "About the fleet map" })).toHaveTextContent("sweeps")
    await user.keyboard("{Escape}")
    expect(fleet.queryByRole("note")).not.toBeInTheDocument()
    expect(about).toHaveFocus()
    await user.click(about)
    await user.click(document.body)
    expect(about).toHaveAttribute("aria-expanded", "false")
  })

  it("counts the work the reader's lens names: with AI only", async () => {
    show(<DashboardView work={["ai"]} />)
    const card = (await screen.findByRole("link", { name: "IAM" })).closest("li") as HTMLElement
    expect(card).toHaveTextContent("3 commits with AI")
    expect(card).toHaveTextContent("1 by hand · 3 with AI (Claude)")
    expect(card).toHaveTextContent("9 by automation · 30 days")
    expect(within(card).getByRole("img", { name: /^Commits with AI per week/ })).toBeInTheDocument()
    expect(screen.getByRole("heading", { name: "Latest commits" })).toBeInTheDocument()
  })

  it("counts every kind by default, automation too, and the latest commits show it", async () => {
    show(<DashboardView />)
    const card = (await screen.findByRole("link", { name: "IAM" })).closest("li") as HTMLElement
    expect(card).toHaveTextContent("13 commits")
    expect(
      within(card).getByRole("img", {
        name: /^Commits per week.*; by automation 0, 0, 0, 0, 0, 0, 0, 0, 0, 4, 0, 5$/,
      }),
    ).toBeInTheDocument()
    expect(card).toHaveTextContent(/Last change \d/)
    expect(screen.getByRole("heading", { name: "Latest commits" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Show commits by the team or automation" })).toBeInTheDocument()
  })

  it("switches the products to a sortable table and remembers the choice", async () => {
    const user = userEvent.setup()
    show(<DashboardView />)
    await user.click(await screen.findByRole("button", { name: "table" }))
    const table = screen.getByRole("table", { name: /Products/ })
    expect(within(table).getByRole("rowheader", { name: "IAM" })).toBeInTheDocument()
    expect(within(table).getByRole("cell", { name: "75%" })).toBeInTheDocument()
    await user.click(within(table).getByRole("button", { name: /Commits, 30 days/ }))
    expect(within(table).getByRole("columnheader", { name: /Commits, 30 days/ })).toHaveAttribute("aria-sort", "descending")
    expect(document.cookie).toContain("scp-view-products=table")
  })

  it("leaves out the pull requests and issues an operator switched off", async () => {
    const off = { ...rules, features: { pulls: false, issues: false } }
    show(
      <RulesProvider rules={off}>
        <MainNav />
        <GroupView group="iam" />
      </RulesProvider>,
      "/groups/iam",
    )
    await screen.findByRole("heading", { level: 1, name: "IAM" })
    const nav = within(screen.getByRole("navigation", { name: "Main" }))
    expect(nav.getByRole("link", { name: "Commits" })).toBeInTheDocument()
    expect(nav.queryByRole("link", { name: "Pull requests" })).not.toBeInTheDocument()
    expect(nav.queryByRole("link", { name: "Issues" })).not.toBeInTheDocument()
    expect(screen.queryByRole("columnheader", { name: /PRs/ })).not.toBeInTheDocument()
    expect(screen.getByRole("columnheader", { name: /Issues/ })).toBeInTheDocument()
    expect(screen.queryByRole("link", { name: /open issues$/ })).not.toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: "Pull requests" })).not.toBeInTheDocument()
  })

  it("filters and sorts the repositories of a group through the URL", async () => {
    const user = userEvent.setup()
    show(<GroupView group="iam" />, "/groups/iam")
    await screen.findByRole("heading", { level: 1, name: "IAM" })
    const table = screen.getByRole("table")
    const names = () =>
      within(table)
        .getAllByRole("row")
        .slice(1)
        .map((r) => within(within(r).getByRole("rowheader")).getAllByRole("link")[0]?.textContent)
    // One owner goes unsaid: the names alone, as the dashboard's table names its products.
    expect(names()).toEqual(["Agent", "Core"])
    await user.click(screen.getByRole("button", { name: /Repository/ }))
    expect(names()).toEqual(["Core", "Agent"])
    expect(screen.getByRole("columnheader", { name: /Repository/ })).toHaveAttribute("aria-sort", "descending")
    // A number sorts the largest first.
    await user.click(screen.getByRole("button", { name: /Issues/ }))
    expect(screen.getByRole("columnheader", { name: /Issues/ })).toHaveAttribute("aria-sort", "descending")
    await user.click(screen.getByRole("button", { name: /Repository/ }))
    await user.type(screen.getByRole("searchbox", { name: "Filter by name" }), "agent")
    expect(names()).toEqual(["Agent"])
    // The row's facts link where they are kept: the licence at SPDX, the issues on /issues, the release on GitHub.
    const [, row] = within(table).getAllByRole("row")
    if (!row) throw new Error("no row")
    expect(within(row).getByRole("link", { name: "APL2" })).toHaveAttribute("href", "https://spdx.org/licenses/Apache-2.0.html")
    expect(within(row).getByRole("link", { name: /open issues$/ })).toHaveAttribute(
      "href",
      expect.stringMatching(/^\/issues\?repo=/),
    )
  })

  it("renders a repository with its descriptor, badges, README and documents", async () => {
    show(<RepoView owner="acme" name="core" />, "/repos/acme/core")
    expect(await screen.findByRole("heading", { level: 1, name: "Core identity" })).toBeInTheDocument()
    expect(await screen.findByText("Hello from the README.")).toBeInTheDocument()
    expect(screen.getByText("@acme/iam")).toBeInTheDocument()
    expect(screen.getByRole("img", { name: "last commit badge" })).toHaveAttribute(
      "src",
      "/badges/repos/acme/core/last-commit.svg",
    )
    expect(await screen.findByRole("link", { name: "Setup" })).toHaveAttribute("href", "/repos/acme/core/docs/docs/setup")
  })

  it("shows a repository's technology, dependencies and internal links from the composed panels", async () => {
    show(
      repoRoute(panels).render({ params: { owner: "acme", name: "core" } } as Ctx<{ owner: string; name: string }>),
      "/repos/acme/core",
    )
    const tech = await screen.findByRole("region", { name: "Technology" })
    expect(await within(tech).findByText("Maven")).toBeInTheDocument()
    expect(within(tech).getByText("java 21")).toBeInTheDocument()
    expect(within(tech).getByRole("link", { name: "acme/lib" })).toHaveAttribute("href", "/repos/acme/lib")
    expect(within(tech).getByRole("link", { name: "acme/sso" })).toHaveAttribute("href", "/repos/acme/sso")
    const deps = screen.getByRole("region", { name: "Dependencies" })
    expect(within(deps).getByRole("link", { name: "no.acme:lib" })).toHaveAttribute("href", "/repos/acme/lib")
    expect(within(deps).getByText("org.junit.jupiter:junit-jupiter")).toBeInTheDocument()
  })

  it("renders a document with its outline", async () => {
    show(<DocView owner="acme" name="core" path="docs/setup.md" />, "/repos/acme/core/docs/docs/setup.md")
    expect(await screen.findByRole("heading", { level: 1, name: "Setup" })).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Install" })).toHaveAttribute("href", "#install")
    expect(screen.getByRole("link", { name: "docs/setup.md on GitHub" })).toBeInTheDocument()
  })

  it("shows a problem with its request id when a panel fails", async () => {
    server.use(
      http.get("/dashboard", ({ response }) =>
        response.untyped(
          Response.json(
            { type: "about:blank", title: "Internal", status: 500, code: "internal", instance: "urn:scp:request:abc" },
            { status: 500 },
          ),
        ),
      ),
    )
    show(<DashboardView />)
    expect(await screen.findByText("Something went wrong on the server")).toBeInTheDocument()
    expect(screen.getByText("Request id: abc")).toBeInTheDocument()
  })
})

describe("commit log", () => {
  function pages() {
    server.use(
      http.get("/commits", ({ query, response }) => {
        const number = Number(query.get("page") ?? 1)
        const size = Number(query.get("limit") ?? 50)
        const total = 55
        const from = (number - 1) * size
        const commits = Array.from({ length: Math.max(0, Math.min(size, total - from)) }, (_, i) => commit(from + i))
        return response(200).json({ commits, page: { number, size, total, pages: Math.ceil(total / size) }, freshness: fresh })
      }),
    )
  }

  it("pages by number, changes the page size and offers new commits on a later page", async () => {
    pages()
    const user = userEvent.setup()
    const qc = client()
    show(<AllCommitsView defaultSize={50} />, "/commits", qc)
    await screen.findByText("Commit 0")
    await waitFor(() => expect(qc.getQueryData(["snapshot"])).toBeDefined())
    const pager = screen.getByRole("navigation", { name: "Pages" })
    expect(within(pager).getByText("1–50 of 55 commits")).toBeInTheDocument()
    expect(within(pager).getByRole("link", { current: "page" })).toHaveTextContent("1")
    await user.click(within(pager).getByRole("link", { name: "Page 2" }))
    expect(await screen.findByText("Commit 54")).toBeInTheDocument()
    expect(screen.queryByText("Commit 0")).not.toBeInTheDocument()
    expect(window.location.search).toBe("?page=2")
    expect(within(screen.getByRole("navigation", { name: "Pages" })).getByText("51–55 of 55 commits")).toBeInTheDocument()

    server.use(http.get("/snapshot", ({ response }) => response(200).json(snapshot(2))))
    await qc.refetchQueries({ queryKey: ["snapshot"] })
    const button = await screen.findByRole("button", { name: "New commits" })
    expect(screen.getByText("Commit 54")).toBeInTheDocument() // the page stays as it is
    await user.click(button)
    await waitFor(() => expect(screen.queryByRole("button", { name: "New commits" })).not.toBeInTheDocument())

    await user.selectOptions(screen.getByRole("combobox", { name: "Per page" }), "25")
    expect(window.location.search).toBe("?page=3&per=25") // the first commit shown, 51, is on page 3 of 25
    expect(await within(screen.getByRole("navigation", { name: "Pages" })).findByText("51–55 of 55 commits")).toBeInTheDocument()
    expect(document.cookie).toContain("scp-per-commits=25")
  })

  it("refetches a one-page view when the snapshot changes", async () => {
    let n = 0
    server.use(
      http.get("/dashboard", ({ response }) =>
        response(200).json({
          work: ["hand", "ai"],
          groups: [],
          health: null,
          repoCount: ++n,
          ungroupedCount: 0,
          latest: [],
          activity: activity(),
          freshness: { fresh: 0, stale: 0, unknown: 0, error: 0 },
          pulls: noPulls,
        }),
      ),
    )
    const qc = client()
    show(<DashboardView />, "/", qc)
    expect(await screen.findByText("1 repository in the catalogue.")).toBeInTheDocument()
    await waitFor(() => expect(qc.getQueryData(["snapshot"])).toBeDefined())
    server.use(http.get("/snapshot", ({ response }) => response(200).json(snapshot(2, false))))
    await qc.refetchQueries({ queryKey: ["snapshot"] })
    expect(await screen.findByText("2 repositories in the catalogue.")).toBeInTheDocument()
    expect(screen.getByText(/loading its catalogue/)).toBeInTheDocument()
  })

  it("follows the event stream and stops polling while it is open", async () => {
    const sources: FakeEventSource[] = []
    class FakeEventSource extends EventTarget {
      onopen: (() => void) | null = null
      onerror: (() => void) | null = null
      closed = false
      constructor(readonly url: string) {
        super()
        sources.push(this)
      }
      close() {
        this.closed = true
      }
      emit(data: unknown) {
        this.dispatchEvent(new MessageEvent("snapshot", { data: JSON.stringify(data) }))
      }
    }
    vi.stubGlobal("EventSource", FakeEventSource)
    try {
      let n = 0
      server.use(
        http.get("/dashboard", ({ response }) =>
          response(200).json({
            work: ["hand", "ai"],
            groups: [],
            health: null,
            repoCount: ++n,
            ungroupedCount: 0,
            latest: [],
            activity: activity(),
            freshness: { fresh: 0, stale: 0, unknown: 0, error: 0 },
            pulls: noPulls,
          }),
        ),
      )
      const qc = client()
      const view = show(<DashboardView />, "/", qc)
      expect(await screen.findByText("1 repository in the catalogue.")).toBeInTheDocument()
      const source = sources[0]
      expect(source?.url).toBe("/api/v1/events")
      source?.onopen?.()
      await waitFor(() => expect(qc.getQueryData(["snapshot"])).toBeDefined())
      await waitFor(() =>
        expect(
          qc
            .getQueryCache()
            .find({ queryKey: ["snapshot"] })
            ?.observers.some((o) => o.options.refetchInterval === false),
        ).toBe(true),
      )
      source?.emit(snapshot(7))
      expect(await screen.findByText("2 repositories in the catalogue.")).toBeInTheDocument()
      expect(qc.getQueryData<{ version: number }>(["snapshot"])?.version).toBe(7)
      view.unmount()
      expect(source?.closed).toBe(true)
    } finally {
      vi.unstubAllGlobals()
    }
  })
})
