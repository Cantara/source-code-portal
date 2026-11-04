import Link from "next/link"
import type { ReactNode } from "react"
import { cn } from "@/lib/cn"
import { kinds } from "@/lib/preferences"
import { levels } from "@/entities/health/health"
import { FindingIcon, findingKinds, ScoreRing, severities } from "@/entities/health/kinds"
import { activities } from "@/entities/repo/health"
import shots from "./shots.json"

/**
 * The guide (`/guide`, design/guide.md): how to read the portal's signals and how to run one. The pictures are screenshots of the
 * made-up catalogue of cmd/scp TestServeForGuide in both themes, kept current by the guide-page skill; every number and rule in the
 * text is the portal's own (the ranges, the kinds and the health levels are imported from where the pages take them).
 */

type Shot = keyof typeof shots.sizes
type Callout = keyof typeof shots.callouts

const docs = "https://github.com/Cantara/source-code-portal/blob/main/docs"

/** findings are when each health finding is found and what it costs a repository (internal/health/health.go), the most costly first. */
const findings: { code: keyof typeof findingKinds; when: string; points: number; also?: string }[] = [
  { code: "dormant", when: "No change by the team for more than 2 years.", points: 30 },
  { code: "failing", when: "The portal cannot update it from GitHub.", points: 25 },
  { code: "unreleased", when: "Changes since the latest release, which is more than 90 days old.", points: 20 },
  { code: "no-licence", when: "A public repository without a licence GitHub detects.", points: 20 },
  { code: "issues", when: "More than 50 open issues and pull requests.", points: 20, also: "more than 20: 10 points" },
  { code: "quiet", when: "No change by the team for more than 6 months.", points: 10 },
]

/** card is the product card's rows, in the order of their callouts. */
const card: { at: Callout; title: string; body: ReactNode }[] = [
  {
    at: "name",
    title: "The product",
    body: "A group of repositories, named and described in the configuration. The name opens the product's page with every repository.",
  },
  {
    at: "score",
    title: "Health",
    body: "The product's health: a ring filled to its score out of 100, in its level's colour, and the level in words when it is not good. Point at it for the levels.",
  },
  {
    at: "work",
    title: "The work",
    body: (
      <>
        The commits of the work that counts in the last 30 days, then every kind: by hand, with AI (and the assistants it names),
        by automation and in sweeps. A coloured square marks each kind that counts.
      </>
    ),
  },
  {
    at: "trend",
    title: "Twelve weeks",
    body: "One bar per week, the oldest on the left, stacked with AI at the foot, by hand above it and automation on top when it counts.",
  },
  {
    at: "people",
    title: "Who, and when",
    body: "The people behind the work, by GitHub login (the full name on hover), most commits first. The light is on while someone worked in the last 30 days; below it, the last change.",
  },
  {
    at: "recent",
    title: "Where",
    body: "The three repositories changed last, with how long ago.",
  },
  {
    at: "findings",
    title: "The exceptions",
    body: "The findings that cost the most points, each with its kind's icon on a tile of its severity (section 5). Point at an icon for what it means and what it costs. Nothing to see says “No exceptions.”",
  },
  {
    at: "shared",
    title: "What they share",
    body: "Said once: the licence and the language at least 80% of the repositories share, and how many have a GitHub release.",
  },
]

function Shot({ name, alt, className }: { name: Shot; alt: string; className?: string }) {
  const { width, height } = shots.sizes[name]
  // At its own size (the capture's CSS pixels) and never larger: smaller on a narrow screen.
  const img = "mx-auto block h-auto max-w-full rounded-lg"
  return (
    <>
      <img
        src={`/guide/${name}-light.webp`}
        alt={alt}
        width={width}
        height={height}
        loading="lazy"
        className={cn(img, "dark:hidden", className)}
      />
      <img
        src={`/guide/${name}-dark.webp`}
        alt={alt}
        width={width}
        height={height}
        loading="lazy"
        className={cn(img, "hidden dark:block", className)}
      />
    </>
  )
}

function Figure({ children, caption, className }: { children: ReactNode; caption?: string; className?: string }) {
  return (
    <figure className={cn("flex w-fit max-w-full flex-col gap-2", className)}>
      <div className="rounded-xl border border-border bg-gradient-to-br from-muted/60 to-background p-2 shadow-sm sm:p-3">
        {children}
      </div>
      {caption && <figcaption className="max-w-prose px-1 text-xs text-muted-foreground">{caption}</figcaption>}
    </figure>
  )
}

/** Marker is a callout's number, on the picture and in front of its explanation. */
function Marker({ n, className }: { n: number; className?: string }) {
  return (
    <span
      className={cn(
        "inline-flex size-6 shrink-0 items-center justify-center rounded-full bg-primary font-mono text-xs font-semibold text-primary-foreground shadow ring-2 ring-background",
        className,
      )}
    >
      {n}
    </span>
  )
}

/** parts are the guide's two halves, for its readers and for the people who run a portal, each a list of numbered sections. */
const parts = [
  {
    title: "Read the portal",
    sections: [
      { id: "signals", title: "Signals, not lists" },
      { id: "telemetry", title: "The fleet at a glance" },
      { id: "fleet", title: "The fleet map" },
      { id: "card", title: "A product card" },
      { id: "health", title: "Health and findings" },
      { id: "work", title: "The work that counts" },
      { id: "lists", title: "Tables and commits" },
      { id: "yours", title: "Make it yours" },
    ],
  },
  {
    title: "Run a portal",
    sections: [
      { id: "run", title: "Run your own portal" },
      { id: "more", title: "Options and documents" },
    ],
  },
]

/** Contents is the guide's table of contents: chips under the hero on a narrow screen, a sticky list beside the page on a wide one. */
function Contents() {
  let n = 0
  return (
    <nav aria-label="Guide" className="text-sm lg:sticky lg:top-20 lg:max-h-[calc(100dvh-6rem)] lg:overflow-y-auto">
      <p className="mb-2 hidden text-xs font-medium tracking-widest text-muted-foreground uppercase lg:block">On this page</p>
      <div className="flex flex-col gap-3 lg:gap-5">
        {parts.map((part) => (
          <div key={part.title}>
            <p className="mb-1.5 font-mono text-[0.6875rem] tracking-widest text-primary uppercase">{part.title}</p>
            <ul className="flex flex-wrap gap-2 lg:flex-col lg:gap-0 lg:border-l lg:border-border">
              {part.sections.map((s) => {
                n++
                return (
                  <li key={s.id}>
                    <a
                      href={`#${s.id}`}
                      className="block rounded-full border border-border bg-background/70 px-3 py-1 text-muted-foreground transition-colors hover:border-primary/50 hover:text-foreground lg:-ml-px lg:rounded-none lg:border-0 lg:border-l-2 lg:border-transparent lg:bg-transparent lg:py-1.5 lg:pl-3 lg:hover:border-primary"
                    >
                      <span className="mr-1.5 font-mono text-xs text-primary">{String(n).padStart(2, "0")}</span>
                      {s.title}
                    </a>
                  </li>
                )
              })}
            </ul>
          </div>
        ))}
      </div>
    </nav>
  )
}

/** Part opens a half of the guide: who it is for, in a line. */
function Part({ n, title, lede }: { n: number; title: string; lede: string }) {
  return (
    <div className="rounded-xl border border-border bg-muted/30 px-5 py-4">
      <p className="font-mono text-xs tracking-widest text-primary uppercase">Part {n}</p>
      <p className="mt-0.5 text-lg font-semibold tracking-tight">{title}</p>
      <p className="mt-1 text-sm text-muted-foreground">{lede}</p>
    </div>
  )
}

function Section({
  id,
  n,
  title,
  lede,
  children,
}: {
  id: string
  n: number
  title: string
  lede: ReactNode
  children: ReactNode
}) {
  return (
    <section
      id={id}
      aria-labelledby={`${id}-title`}
      className="scroll-mt-20 border-t border-border/60 pt-10 first:border-t-0 first:pt-0"
    >
      <p className="font-mono text-xs tracking-widest text-primary uppercase">{String(n).padStart(2, "0")}</p>
      <h2 id={`${id}-title`} className="mt-1 text-xl font-semibold tracking-tight sm:text-2xl">
        {title}
      </h2>
      <p className="mt-2 max-w-prose text-muted-foreground">{lede}</p>
      <div className="mt-6 flex flex-col gap-6">{children}</div>
    </section>
  )
}

function Terms({ items }: { items: [string, ReactNode][] }) {
  return (
    <dl className="grid gap-x-8 gap-y-4 sm:grid-cols-2 lg:grid-cols-3">
      {items.map(([t, d]) => (
        <div key={t} className="border-l-2 border-primary/40 pl-3">
          <dt className="text-sm font-semibold">{t}</dt>
          <dd className="mt-0.5 text-sm text-muted-foreground">{d}</dd>
        </div>
      ))}
    </dl>
  )
}

function Code({ children, label }: { children: string; label: string }) {
  return (
    <pre
      aria-label={label}
      tabIndex={0}
      className="overflow-x-auto rounded-lg border border-border bg-muted/50 p-4 font-mono text-xs leading-relaxed sm:text-[0.8125rem]"
    >
      <code>{children}</code>
    </pre>
  )
}

/**
 * AnnotatedCard is the card's picture with its numbers, as one SVG of the picture's size and a gutter on its left (so the numbers
 * scale with it and never cover the card): a number in the gutter with a leader to its row, or beside the row when it sits on the
 * right. The positions are measured by capture.js. No inline style: the CSP forbids it.
 */
function AnnotatedCard({ alt }: { alt: string }) {
  const { width, height } = shots.sizes.card
  const gutter = 44
  return (
    <svg
      role="img"
      aria-label={alt}
      viewBox={`${-gutter} 0 ${width + gutter} ${height}`}
      width={width + gutter}
      height={height}
      className="block h-auto max-w-full"
    >
      <image href="/guide/card-light.webp" width={width} height={height} className="dark:hidden" />
      <image href="/guide/card-dark.webp" width={width} height={height} className="hidden dark:block" />
      {card.map((c, i) => {
        const at = shots.callouts[c.at]
        const ax = (at.x / 100) * width
        const y = (at.y / 100) * height
        const right = at.x > 50
        const x = right ? ax - 16 : -gutter / 2
        return (
          <g key={c.at}>
            {!right && (
              <line
                x1={x + 12}
                y1={y}
                x2={ax - 4}
                y2={y}
                strokeWidth={1.5}
                strokeDasharray="3 3"
                className="stroke-primary opacity-60"
              />
            )}
            <circle cx={x} cy={y} r={11} strokeWidth={2.5} className="fill-primary stroke-background" />
            <text
              x={x}
              y={y}
              dy="0.35em"
              textAnchor="middle"
              className="fill-primary-foreground font-mono text-[11px] font-semibold"
            >
              {i + 1}
            </text>
          </g>
        )
      })}
    </svg>
  )
}

const dots = { hand: "bg-primary", ai: "bg-[var(--ai)]", automation: "bg-muted-foreground" } as const

/** GuideView is the guide; version is the portal's release, for the commands (a placeholder on a development build). */
export function GuideView({ version }: { version: string }) {
  const tag = /^v?\d+\.\d+\.\d+$/.test(version) ? version.replace(/^v/, "") : "<version>"
  return (
    <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_13rem] lg:gap-x-12 xl:grid-cols-[minmax(0,1fr)_15rem]">
      <header className="relative overflow-hidden rounded-2xl border border-border bg-gradient-to-br from-primary/10 via-background to-[color-mix(in_oklch,var(--ai)_10%,transparent)] px-5 py-8 sm:px-10 sm:py-10 lg:col-start-1">
        <p className="font-mono text-xs tracking-widest text-primary uppercase">Getting started</p>
        <h1
          tabIndex={-1}
          className="mt-2 max-w-3xl text-3xl font-semibold tracking-tight text-balance focus:outline-none sm:text-4xl"
        >
          Read the portal in five minutes, run your own in thirty
        </h1>
        <p className="mt-4 max-w-2xl text-base text-muted-foreground">
          Source Code Portal turns every repository of your GitHub organisations into a few signals: who works where, what needs a
          look, and what automation keeps alive. The first part explains how to read them; the second how to run a portal on your
          own repositories.
        </p>
        <div className="mt-6 grid max-w-2xl gap-3 sm:grid-cols-2">
          {[
            ["#signals", "Part 1", "Read the portal", "The dashboard, a product card, health and the work that counts."],
            ["#run", "Part 2", "Run a portal", "Access to GitHub, your products, checks, the container and the options."],
          ].map(([href, n, t, d]) => (
            <a key={href} href={href} className="panel flex flex-col gap-0.5 p-4 transition-colors hover:border-primary/50">
              <span className="font-mono text-[0.6875rem] tracking-widest text-primary uppercase">{n}</span>
              <span className="text-sm font-semibold">{t} →</span>
              <span className="text-xs text-muted-foreground">{d}</span>
            </a>
          ))}
        </div>
      </header>

      <aside className="lg:col-start-2 lg:row-span-2 lg:row-start-1">
        <Contents />
      </aside>

      <div className="flex min-w-0 flex-col gap-12 lg:col-start-1">
        <Part
          n={1}
          title="Read the portal"
          lede="For everyone who opens the dashboard: what each signal says and where to look first."
        />
        <Section
          id="signals"
          n={1}
          title="Signals, not lists"
          lede="The dashboard does not list every attribute of every repository. It shows what an engineer acts on and stays quiet about the rest."
        >
          <div className="grid gap-4 md:grid-cols-3">
            {[
              [
                "Exceptions, not defaults",
                "A product card says once what most of its repositories share and shows only what stands out.",
              ],
              ["Work, by kind", "People by hand, people with AI and automation are counted apart, so neither hides the other."],
              [
                "Always current",
                "The portal follows GitHub through webhooks and its own schedule, and says when something is late.",
              ],
            ].map(([t, d]) => (
              <div key={t} className="panel p-4">
                <p className="text-sm font-semibold">{t}</p>
                <p className="mt-1 text-sm text-muted-foreground">{d}</p>
              </div>
            ))}
          </div>
          <p className="max-w-prose text-sm text-muted-foreground">
            Every coloured signal explains itself: point at it, or tap it on a phone, and a small card names it and its values.
          </p>
        </Section>

        <Section
          id="telemetry"
          n={2}
          title="The fleet at a glance"
          lede="The row of tiles at the top of the dashboard: every repository of the portal, summed up. A light shows a state worth knowing."
        >
          <Figure caption="The telemetry of a portal with three products and thirteen repositories.">
            <Shot name="telemetry" alt="Six tiles: People 5, Active 12, Automation 57, Findings 8, Products 3, In sync 100%." />
          </Figure>
          <Terms
            items={[
              ["People", "Who made the work that counts in the last 30 days, and how many commits were by hand and with AI."],
              ["Active", "Repositories the work that counts changed in the last 30 days, of all of them."],
              ["Automation", "Commits by CI, dependency and release bots in 30 days, and the commits of sweeps."],
              ["Findings", "Everything the health checks found, and how many products need a look."],
              ["Products", "The groups of the configuration, and the repositories in none."],
              ["In sync", "The share of repositories whose data is current; a red light when updates from GitHub fail."],
            ]}
          />
        </Section>

        <Section
          id="fleet"
          n={3}
          title="The fleet map"
          lede="Every repository is one tile, grouped by product and coloured by the age of its last change. Big products look big; cold ones look cold."
        >
          <Figure caption="Point at a tile, or tap it, for its repository, its age and what its colour means; a click opens it. The legend explains each colour the same way.">
            <Shot name="fleet" alt="The fleet map: a row of coloured tiles per product." />
          </Figure>
          <ul aria-label="The colours of the fleet map" className="flex flex-wrap gap-x-6 gap-y-3 text-sm">
            {activities.map((a) => (
              <li key={a.level} className={cn(`lvl-${a.level}`, "flex items-center gap-2")}>
                <span aria-hidden="true" className="cell" />
                <span className="font-medium">{a.label}</span>
                <span className="font-mono text-xs text-muted-foreground">{a.span || "on GitHub"}</span>
              </li>
            ))}
          </ul>
          <p className="max-w-prose text-sm text-muted-foreground">
            The age is of the last change of the work that counts (section 6), every kind until you choose: a bot&apos;s
            dependency update keeps a repository alive too. Count people&apos;s work alone to see which ones people still change.
          </p>
        </Section>

        <Section
          id="card"
          n={4}
          title="A product card"
          lede="Each product gets the same rows on every card, so the eye learns where to look. The numbers below match the picture."
        >
          <div className="grid items-start gap-8 xl:grid-cols-[minmax(0,27rem)_1fr]">
            <Figure className="xl:sticky xl:top-20">
              <AnnotatedCard alt="The Payments card, its rows numbered 1 to 8: 53 commits, 6 by hand, 20 with AI, 22 by automation and 5 in sweeps, its people, its recent repositories and three findings." />
            </Figure>
            <ol className="flex max-w-prose flex-col gap-4">
              {card.map((c, i) => (
                <li key={c.at} className="flex gap-3">
                  <Marker n={i + 1} className="mt-0.5" />
                  <div>
                    <p className="text-sm font-semibold">{c.title}</p>
                    <p className="text-sm text-muted-foreground">{c.body}</p>
                  </div>
                </li>
              ))}
            </ol>
          </div>
        </Section>

        <Section
          id="health"
          n={5}
          title="Health and findings"
          lede="Each repository starts at 100 and loses the points of its findings. A product weighs its repositories into one score."
        >
          <ul aria-label="Health levels" className="grid gap-3 sm:grid-cols-3">
            {(
              [
                ["good", 92, "80 to 100", "The ring alone: nothing to do."],
                ["watch", 68, "50 to 79", "The ring and the word: worth a look."],
                ["attention", 38, "below 50", "Also when any repository of the product is."],
              ] as const
            ).map(([l, score, range, note]) => (
              <li key={l} className={cn(levels[l].className, "panel flex items-center gap-3 p-3")}>
                <ScoreRing score={score} large />
                <span className="flex flex-col">
                  <span className="text-sm font-semibold">{levels[l].label}</span>
                  <span className="font-mono text-xs">{range}</span>
                  <span className="text-xs text-muted-foreground">{note}</span>
                </span>
              </li>
            ))}
          </ul>
          <div className="panel overflow-x-auto">
            <table className="w-full text-sm">
              <caption className="px-4 pt-3 text-left text-xs font-medium tracking-widest text-muted-foreground uppercase">
                The findings, the most costly first
              </caption>
              <thead>
                <tr className="border-b border-border text-xs text-muted-foreground">
                  <th scope="col" className="w-10 px-4 py-2 text-left font-medium">
                    <span className="sr-only">Icon</span>
                  </th>
                  <th scope="col" className="px-2 py-2 text-left font-medium">
                    Finding
                  </th>
                  <th scope="col" className="hidden px-2 py-2 text-left font-medium sm:table-cell">
                    When
                  </th>
                  <th scope="col" className="px-4 py-2 text-right font-medium">
                    Points
                  </th>
                </tr>
              </thead>
              <tbody>
                {findings.map((f) => (
                  <tr key={f.code} className="border-b border-border/60 align-top last:border-b-0">
                    <td className="px-4 py-3">
                      <FindingIcon code={f.code} penalty={f.points} />
                    </td>
                    <th scope="row" className="px-2 py-3 text-left font-normal">
                      <span className="block font-semibold">{findingKinds[f.code].label}</span>
                      <span className="block text-muted-foreground">{findingKinds[f.code].about}</span>
                      <span className="mt-1 block text-xs text-muted-foreground sm:hidden">
                        {f.when}
                        {f.also && ` (${f.also})`}
                      </span>
                    </th>
                    <td className="hidden px-2 py-3 text-muted-foreground sm:table-cell">
                      {f.when}
                      {f.also && <span className="block text-xs">{f.also}</span>}
                    </td>
                    <td className="px-4 py-3 text-right font-mono whitespace-nowrap tabular-nums">−{f.points}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <ul aria-label="Severity" className="flex flex-wrap gap-x-6 gap-y-2 text-sm">
            {(
              [
                ["high", 30, "25 points or more"],
                ["medium", 20, "20 points"],
                ["low", 10, "10 points"],
              ] as const
            ).map(([sev, , note]) => (
              <li key={sev} className="flex items-center gap-2">
                <span aria-hidden="true" className={cn("finding-icon", severities[sev].className)} />
                <span className="font-medium">{severities[sev].label}</span>
                <span className="text-xs text-muted-foreground">{note}</span>
              </li>
            ))}
          </ul>
          <div className="grid items-start gap-8 xl:grid-cols-[1fr_minmax(0,25rem)]">
            <div className="flex max-w-prose flex-col gap-3 text-sm text-muted-foreground">
              <p>
                The icon says what a finding is, the colour of its tile how much it weighs: red is high, amber medium, grey low.
                On a card, point at an icon or tap it for its name, what it costs and why it matters.
              </p>
              <p>
                The age picks a penalty, it does not grow with it: four years and ten cost the same 30 points. The product&apos;s
                score is the weighted mean of its repositories: one changed in the last 6 months counts twice, the product&apos;s
                main repository three times more. So a forgotten side repository costs a product little, and a neglected main one
                costs it a lot. Archived and deprecated repositories are not scored.
              </p>
            </div>
            <Figure caption="A product to watch: its main repository has 56 open issues and unreleased changes.">
              <Shot
                name="card-watch"
                alt="The Developer tools card with the health score 74 and the level Watch, and four findings."
              />
            </Figure>
          </div>
        </Section>

        <Section
          id="work"
          n={6}
          title="The work that counts"
          lede="People and coding agents work together, and automation keeps the lights on. The portal tells them apart and ranks none above the others."
        >
          <ul className="grid gap-4 md:grid-cols-3">
            {kinds.map((k) => (
              <li key={k.value} className="panel flex flex-col gap-1 p-4">
                <span className="flex items-center gap-2 text-sm font-semibold">
                  <span aria-hidden="true" className={cn("size-2.5 rounded-xs", dots[k.value])} />
                  {k.label}
                </span>
                <span className="text-sm text-muted-foreground">{k.about}</span>
              </li>
            ))}
          </ul>
          <div className="grid items-start gap-8 xl:grid-cols-[1fr_minmax(0,30rem)]">
            <div className="flex max-w-prose flex-col gap-3 text-sm text-muted-foreground">
              <p>
                A commit is <strong className="text-foreground">with AI</strong> when it carries an AI co-author (the{" "}
                <code className="font-mono text-xs">Co-authored-by</code> trailer Claude Code, Copilot and Cursor write) or an AI
                tool&apos;s footer, or when a coding agent made it. Inline completion leaves no trace, so this is a lower bound.
              </p>
              <p>
                A commit is <strong className="text-foreground">by automation</strong> when a bot made it (Renovate, Dependabot,
                GitHub Actions, a release plugin), or when it is part of a sweep: one change landing in more than 10 repositories
                on one day. Automation names no people: the people on a card are always people.
              </p>
              <p>
                <Link href="/settings" className="font-medium text-primary hover:underline">
                  Settings
                </Link>{" "}
                choose what the dashboard counts, any of the three; all three until you choose. People&apos;s work alone is by
                hand and with AI, a maintenance view automation alone. The other kinds stay in sight on every card.
              </p>
            </div>
            <Figure caption="Settings: the work that counts.">
              <Shot name="settings" alt="The Work that counts settings: checkboxes for by hand, with AI and by automation." />
            </Figure>
          </div>
        </Section>

        <Section
          id="lists"
          n={7}
          title="Tables and commits"
          lede="Many products compare better in one table; the latest commits show what just happened."
        >
          <Figure caption="The products as a table: every column sorts. The choice of cards or table is remembered.">
            <Shot
              name="table"
              alt="The products table with columns for repositories, commits, share with AI, automation, people, last change, findings and health."
            />
          </Figure>
          <div className="grid items-start gap-8 xl:grid-cols-[minmax(0,26rem)_1fr]">
            <Figure caption="The latest commits, beside the products on a wide screen.">
              <Shot
                name="latest"
                alt="The latest commits: subject, repository, author and age, the newest five by dependency and CI bots."
              />
            </Figure>
            <div className="flex max-w-prose flex-col gap-3 text-sm text-muted-foreground">
              <p>
                The latest commits of the work that counts sit in a panel beside the products: fold it away with its button, or
                drag its edge to make it wider. Commits made with AI carry a mark naming the assistant.
              </p>
              <p>
                <strong className="text-foreground">Commits</strong> in the header holds every commit of every repository, to
                filter by kind, product and author. Each repository has its own page with its README, documents, dependencies and
                its history as a git graph.
              </p>
            </div>
          </div>
        </Section>

        <Section
          id="yours"
          n={8}
          title="Make it yours"
          lede="What you choose is kept in your browser, never on the server, and every page follows it."
        >
          <Terms
            items={[
              ["The work that counts", "By hand, with AI, by automation: any of them (section 6)."],
              ["Cards or a table", "The products' view, switched above them."],
              ["The side panel", "The latest commits folded away or as wide as you dragged them."],
              ["Page sizes", "How many commits a list shows at a time."],
              ["Theme", "System, dark or light, from the header's theme menu."],
              ["Sign in", "Where the portal has sign-in, private repositories show for the groups allowed to see them."],
            ]}
          />
        </Section>

        <Part
          n={2}
          title="Run a portal"
          lede="For the people who put a portal on their own repositories: from nothing to a running portal, then the options."
        />
        <Section
          id="run"
          n={9}
          title="Run your own portal"
          lede="One binary or one container, one YAML file and read access to GitHub. Nothing to back up but the configuration and its secrets."
        >
          <ol className="flex flex-col gap-6">
            <Step n={1} title="Give it read access to GitHub">
              <p>
                A GitHub App with <em>Contents</em> and <em>Metadata</em> read-only (its own rate limit, and webhooks for updates
                within seconds), or a personal access token in a file. Secrets are always files, never values in the
                configuration.
              </p>
            </Step>
            <Step n={2} title="Describe your products">
              <p>
                Groups gather repositories into products by name prefix, glob, regular expression, exact name or GitHub topic;{" "}
                <code className="font-mono text-xs">unclaimed</code> catches the rest.
              </p>
              <Code label="scp.yaml">{`github:
  sources:
    - id: github
      orgs: [your-org]
      token_file: /run/secrets/github-token
catalog:
  groups:
    - id: payments
      name: Payments
      description: Checkout, the ledger and the card gateway
      default_repo: payments-api
      match: [{ prefix: payments- }]
    - { id: other, name: Other, default_repo: handbook, match: [{ unclaimed: true }] }`}</Code>
            </Step>
            <Step n={3} title="Check before you run">
              <Code label="Check the configuration">{`scp config check -c scp.yaml           # every problem with its key, the secrets redacted
gh api --paginate "orgs/your-org/repos?per_page=100" > repos.json
scp config groups -c scp.yaml --repos repos.json   # where each repository lands`}</Code>
            </Step>
            <Step n={4} title="Run it">
              <Code label="Run the container">{`docker run -d --name scp --read-only --cap-drop=ALL \\
  -p 9090:9090 -p 127.0.0.1:9091:9091 \\
  -v scp-data:/data -v "$PWD/scp.yaml:/etc/scp/scp.yaml:ro" -v "$PWD/secrets:/run/secrets:ro" \\
  ghcr.io/cantara/source-code-portal:${tag} serve --config /etc/scp/scp.yaml`}</Code>
              <p>
                Port 9090 is the portal, behind your reverse proxy; 9091 holds <code className="font-mono text-xs">/readyz</code>,{" "}
                <code className="font-mono text-xs">/metrics</code> and the status, inside your network. The first start reads
                every repository and the pages say so meanwhile.
              </p>
            </Step>
          </ol>
        </Section>

        <Section
          id="more"
          n={10}
          title="Options and documents"
          lede="Add what you need when you need it; the documents cover every key."
        >
          <ul className="grid gap-2 text-sm text-muted-foreground sm:grid-cols-2">
            {[
              ["Webhooks", "pushes show within seconds"],
              ["Sign-in", "single sign-on through your OpenID Connect provider, and access tokens"],
              ["Private repositories", "shown only to the groups you name"],
              ["The assistant", "questions about the catalogue in plain language, with your model provider"],
              ["The design mode", "administrators arrange the dashboard for everyone"],
              ["Several instances", "one portal served together through Hazelcast"],
            ].map(([t, d]) => (
              <li key={t} className="rounded-lg border border-border p-3">
                <span className="font-medium text-foreground">{t}</span>: {d}
              </li>
            ))}
          </ul>
          <div className="grid gap-4 md:grid-cols-3">
            {[
              [
                `${docs}/production.md`,
                "Production setup",
                "From nothing to a public portal: one host, Docker, Caddy, a GitHub App, sign-in.",
              ],
              [
                `${docs}/operator-guide.md`,
                "Operator guide",
                "Every key, running, several instances, monitoring and troubleshooting.",
              ],
              [
                `${docs}/repository-guide.md`,
                "Repository guide",
                "What the portal reads from a repository, and its scp.yaml descriptor.",
              ],
            ].map(([href, t, d]) => (
              <a
                key={t}
                href={href}
                className="panel panel-live flex flex-col gap-1 p-4 transition-colors hover:border-primary/50"
              >
                <span className="text-sm font-semibold">{t} →</span>
                <span className="text-sm text-muted-foreground">{d}</span>
              </a>
            ))}
          </div>
          <div className="rounded-xl border border-dashed border-border p-5 text-sm text-muted-foreground">
            <p className="font-semibold text-foreground">Let Claude Code set it up with you</p>
            <p className="mt-1">
              The portal&apos;s plugin for Claude Code knows the configuration and the deployment of every release: it writes the{" "}
              <code className="font-mono text-xs">scp.yaml</code> from your wishes, proves it with the portal&apos;s own checks
              and walks you through the deployment.
            </p>
            <Code label="Install the plugin in Claude Code">{`/plugin install scp --marketplace Cantara/source-code-portal`}</Code>
          </div>
        </Section>
      </div>
    </div>
  )
}

/** Step is one step of running a portal; its children are blocks (paragraphs, code). */
function Step({ n, title, children }: { n: number; title: string; children: ReactNode }) {
  return (
    <li className="flex gap-4">
      <span className="flex size-8 shrink-0 items-center justify-center rounded-full border border-primary/40 bg-primary/10 font-mono text-sm font-semibold text-primary">
        {n}
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-3 text-sm text-muted-foreground">
        <p className="text-base font-semibold text-foreground">{title}</p>
        {children}
      </div>
    </li>
  )
}
