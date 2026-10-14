import { describe, expect, it } from "vitest"
import { renderToStaticMarkup } from "react-dom/server"
import { columnHelper, DataTable, nextSortToken, sortingFromToken } from "./data-table"

interface Row { readonly slug: string; readonly issues: number }
const helper = columnHelper<Row>()
const columns = helper.columns([
  helper.accessor("slug", { id: "name", header: "Repository", cell: (c) => c.getValue() }),
  helper.accessor("issues", { id: "openIssues", header: "Open issues", cell: (c) => c.getValue() }),
])

describe("sort tokens", () => {
  it("round-trips the API token", () => {
    expect(sortingFromToken("-pushedAt")).toEqual([{ id: "pushedAt", desc: true }])
    expect(sortingFromToken("name")).toEqual([{ id: "name", desc: false }])
    expect(sortingFromToken(undefined)).toEqual([])
  })
  it("first activation ascending, second descending, then ascending again", () => {
    expect(nextSortToken(undefined, "name")).toBe("name")
    expect(nextSortToken("name", "name")).toBe("-name")
    expect(nextSortToken("-name", "name")).toBe("name")
    expect(nextSortToken("name", "openIssues")).toBe("openIssues")
  })
  it("a column whose natural order is descending starts descending (lastCommit, pushedAt, openIssues; V-05)", () => {
    expect(nextSortToken("name", "lastCommit", true)).toBe("-lastCommit")
    expect(nextSortToken("-lastCommit", "lastCommit", true)).toBe("lastCommit")
    expect(nextSortToken("lastCommit", "lastCommit", true)).toBe("-lastCommit")
    expect(nextSortToken(undefined, "openIssues", true)).toBe("-openIssues")
  })
})

describe("DataTable (server render)", () => {
  const rows: Row[] = [{ slug: "core-lib", issues: 3 }, { slug: "auth-client", issues: 0 }]
  it("renders caption, scoped headers, aria-sort and rows in the given order (the API sorted them)", () => {
    const html = renderToStaticMarkup(<DataTable columns={columns} rows={rows} getRowId={(r) => r.slug} caption="Repositories of group Platform" sort="-openIssues" onSortChange={() => {}} />)
    expect(html).toContain('<caption class="sr-only">Repositories of group Platform</caption>')
    expect(html).toContain('scope="col" aria-sort="none"')
    expect(html).toContain('aria-sort="descending"')
    expect(html.indexOf("core-lib")).toBeLessThan(html.indexOf("auth-client"))
  })
  it("renders the empty slot instead of an empty table", () => {
    const html = renderToStaticMarkup(<DataTable columns={columns} rows={[]} getRowId={(r) => r.slug} caption="x" empty={<p>None</p>} />)
    expect(html).toBe("<p>None</p>")
  })
})

describe("DataTable column meta", () => {
  it("defaultDesc is readable from the column definition (the click handler passes it to nextSortToken)", () => {
    const h = columnHelper<Row>()
    const cols = h.columns([h.accessor("issues", { id: "openIssues", header: "Open issues", meta: { defaultDesc: true } })])
    expect(cols[0]?.meta?.defaultDesc).toBe(true)
  })
})
