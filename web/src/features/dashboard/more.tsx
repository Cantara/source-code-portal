"use client"

import { useInfiniteQuery, type InfiniteData, type UseInfiniteQueryOptions } from "@tanstack/react-query"
import { useEffect, useRef, useState, type RefObject } from "react"
import { morePages } from "./api"

/**
 * useMore is a section's list that goes on as it is scrolled (design/layout.md, "Loads more at the end"), as the latest commits do:
 * the first items come from the element's own query; once the list's end (end) comes into view it reads the same list page by
 * page, up to morePages pages, and items has them all once each (key). The caller holds end, the element after the list. done: nothing more to read, the page's link has the rest.
 * loaded: the first page is in. reset forgets the pages read (a list that went back to its top).
 */
export function useMore<T, P extends { page: { number: number; pages: number } }>(
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- the options of any infinite query of pages
  opts: UseInfiniteQueryOptions<P, Error, InfiniteData<P>, any, number>,
  first: T[],
  of: (p: P) => T[],
  key: (t: T) => string,
  end: RefObject<HTMLElement | null>,
  watch: boolean,
  /** read from the first render, without waiting for the end to come near */
  start = false,
) {
  const [more, setMore] = useState(start)
  const q = useInfiniteQuery({ ...opts, enabled: more })
  const pages = more ? (q.data?.pages.length ?? 0) : 0
  const seen = new Set<string>()
  const items = [...first, ...(more ? (q.data?.pages.flatMap(of) ?? []) : [])].filter((t) => {
    const k = key(t)
    if (seen.has(k)) return false
    seen.add(k)
    return true
  })
  const next = useRef(() => {})
  useEffect(() => {
    next.current = () => {
      if (!more) setMore(true)
      else if (q.hasNextPage && !q.isFetching && pages < morePages) void q.fetchNextPage()
    }
  })
  // The end of the list near the view (in the section's own scroll, or the page's on a phone) reads the next page.
  useEffect(() => {
    const el = end.current
    if (!watch || !el || typeof IntersectionObserver === "undefined") return // without it the list stays, the link has the rest
    const o = new IntersectionObserver(
      (es) => {
        if (es.some((e) => e.isIntersecting)) next.current()
      },
      // The margin is the scroll's that holds the list, so the next page is read before its end is reached.
      { root: scroller(el), rootMargin: "0px 0px 300px 0px" },
    )
    o.observe(el)
    return () => o.disconnect()
  }, [end, watch])
  // A page that left the end in view (a tall section) reads the next at once. The end's place is measured now: the observer only
  // reports a change, so what it last said is stale once the page has made the list longer.
  useEffect(() => {
    const el = end.current
    if (more && watch && !q.isFetching && el && near(el)) next.current()
  }, [end, more, watch, q.isFetching, pages])
  const done = (more && q.isSuccess && !q.hasNextPage) || pages >= morePages
  return {
    items,
    loading: more && q.isFetching && q.data !== undefined,
    done,
    loaded: q.data !== undefined,
    reset: () => setMore(false),
  }
}

/** near says whether el is within 300 px below the bottom of the area that scrolls it (the page, else), as the observer's margin. */
function near(el: Element): boolean {
  const root = scroller(el)
  const bottom = root ? root.getBoundingClientRect().bottom : window.innerHeight
  return el.getBoundingClientRect().top <= bottom + 300
}

/** scroller is the nearest element that scrolls el, else none: the page. */
export function scroller(el: Element): Element | null {
  for (let p = el.parentElement; p; p = p.parentElement) {
    const o = getComputedStyle(p).overflowY
    if (o === "auto" || o === "scroll") return p
  }
  return null
}
