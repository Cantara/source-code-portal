// Server-sorted table (compile-checked against @tanstack/react-table 9.2.6). Rows are already filtered, sorted and paged by the API
// (manualSorting), so the table only owns presentation: header buttons with aria-sort, a visually hidden caption, empty state slot.
import { createColumnHelper, flexRender, rowSortingFeature, tableFeatures, useTable, type ColumnDef, type RowData, type SortingState } from "@tanstack/react-table"
import type { ReactNode } from "react"

const features = tableFeatures({ rowSortingFeature })

/** Column authoring for pages: `const h = columnHelper<RepoSummary>(); const columns = h.columns([h.accessor("slug", { id: "name", header: ... })])`. */
export const columnHelper = <T extends RowData>() => createColumnHelper<typeof features, T>()

export interface DataTableProps<T extends RowData> {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- the helper widens each column value type to `any`; that is the documented v9 pattern
  readonly columns: ColumnDef<typeof features, T, any>[]
  readonly rows: readonly T[]
  readonly getRowId: (row: T) => string
  /** Visually hidden but announced: "Repositories of group Platform". Required, a table without a name is an axe violation in practice. */
  readonly caption: string
  /** Server sort as the API token (`name`, `-pushedAt`); the column id is the token without the sign. */
  readonly sort?: string
  readonly onSortChange?: (token: string) => void
  readonly empty?: ReactNode
}

/** `-pushedAt` -> [{ id: "pushedAt", desc: true }]. */
export function sortingFromToken(token: string | undefined): SortingState {
  if (!token) return []
  return token.startsWith("-") ? [{ id: token.slice(1), desc: true }] : [{ id: token, desc: false }]
}
/**
 * Next token when a sortable header is activated: first ascending, then descending, then ascending again (no "unsorted" state: the API always sorts).
 * A column whose natural order is descending (`pushedAt`, `lastCommit`, `openIssues`: newest and biggest first) passes `descFirst`: its first activation
 * is `-id`, then `id`, then `-id` (V-05: the first draft had no such parameter, so the documented "starts descending" behaviour did not exist).
 */
export function nextSortToken(current: string | undefined, columnId: string, descFirst = false): string {
  if (current === columnId) return `-${columnId}`
  if (current === `-${columnId}`) return columnId
  return descFirst ? `-${columnId}` : columnId
}

/** Column meta read by `DataTable`: `{ defaultDesc: true }` on a column makes its first sort descending. */
declare module "@tanstack/react-table" {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars -- module augmentation signature must repeat the generics
  interface ColumnMeta<TFeatures, TData extends RowData, TValue> { readonly defaultDesc?: boolean }
}

export function DataTable<T extends RowData>(p: DataTableProps<T>) {
  const table = useTable<typeof features, T>({
    features,
    columns: p.columns,
    data: p.rows as T[],
    getRowId: p.getRowId,
    manualSorting: true,
    enableSortingRemoval: false,
    state: { sorting: sortingFromToken(p.sort) },
  })
  if (p.rows.length === 0 && p.empty) return <>{p.empty}</>
  return (
    <table>
      <caption className="sr-only">{p.caption}</caption>
      <thead>
        {table.getHeaderGroups().map((hg) => (
          <tr key={hg.id}>
            {hg.headers.map((h) => {
              const sorted = h.column.getIsSorted()
              const sortable = h.column.getCanSort() && p.onSortChange !== undefined
              return (
                <th key={h.id} scope="col" aria-sort={sorted === "asc" ? "ascending" : sorted === "desc" ? "descending" : sortable ? "none" : undefined}>
                  {sortable ? (
                    <button type="button" onClick={() => p.onSortChange?.(nextSortToken(p.sort, h.column.id, h.column.columnDef.meta?.defaultDesc === true))}>
                      {flexRender(h.column.columnDef.header, h.getContext())}
                    </button>
                  ) : (
                    flexRender(h.column.columnDef.header, h.getContext())
                  )}
                </th>
              )
            })}
          </tr>
        ))}
      </thead>
      <tbody>
        {table.getRowModel().rows.map((r) => (
          <tr key={r.id}>
            {r.getAllCells().map((c) => (
              <td key={c.id}>{flexRender(c.column.columnDef.cell, c.getContext())}</td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  )
}
