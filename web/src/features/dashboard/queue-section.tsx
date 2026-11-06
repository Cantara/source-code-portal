"use client"

import Link from "next/link"
import type { ReactNode, RefObject } from "react"
import { Card } from "@/components/ui/card"
import { cn } from "@/lib/cn"
import { ColumnSection, type Row } from "./column"
import { sectionBody, sectionCard, SectionFold, SectionTitle, type Section } from "./side"

/** queueKey is one pull request or issue once, however many pages it came in. */
export const queueKey = (x: { repo: string; number: number }) => `${x.repo}#${x.number}`

/**
 * QueueSection is what the pull requests and the issues elements share: a section of the dashboard's side panel (a card whose title
 * folds it, a line that sums it up, its list scrolling inside it) or, as column, a section of the presentation page's side column
 * drawn as its latest commits are (rows). Either way its end reads the next page (more.tsx), then links to every one.
 */
export function QueueSection({
  id,
  title,
  all,
  section,
  column,
  summary,
  list,
  rows,
  end,
  loading,
  done,
  empty,
  rest,
}: {
  id: string
  title: string
  /** the count's link: the page with every one */
  all: { href: string; text: string }
  section?: Section
  column: boolean
  /** the card's line under its title: how many wait for what */
  summary: string
  /** the card's list, drawn by the entity */
  list: ReactNode
  /** the column's rows */
  rows: Row[]
  end: RefObject<HTMLDivElement | null>
  loading: boolean
  done: boolean
  empty: string
  /** the words at the end once nothing more is read */
  rest: string
}) {
  if (column) {
    return (
      <ColumnSection
        id={id}
        title={title}
        all={all}
        section={section}
        rows={rows}
        end={end}
        loading={loading}
        done={done}
        empty={empty}
        rest={rest}
      />
    )
  }
  return (
    <Card aria-labelledby={id} className={cn("flex flex-col gap-2", sectionCard(section))}>
      <div className="flex items-center gap-2">
        <SectionTitle id={id}>{title}</SectionTitle>
        <Link href={all.href} className="text-sm whitespace-nowrap text-muted-foreground hover:text-foreground hover:underline">
          {all.text}
        </Link>
        <SectionFold id={id} section={section} />
      </div>
      {section?.open !== false && <p className="text-sm text-muted-foreground">{summary}</p>}
      {section?.open !== false && (
        <div id={`${id}-body`} className={cn("flex flex-col gap-2", sectionBody(section))}>
          {list}
          <div ref={end} aria-live="polite" className="text-center text-xs text-muted-foreground">
            {loading ? "Loading more…" : null}
            {done ? (
              <Link href={all.href} className="hover:text-primary hover:underline">
                {rest}
              </Link>
            ) : null}
          </div>
        </div>
      )}
    </Card>
  )
}
