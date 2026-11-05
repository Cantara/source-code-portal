import type { ReactNode } from "react"

/** Icon draws a 24-unit line icon in the current colour, hidden from assistive technology: its control carries the name. */
function Icon({ children, className = "size-4" }: { children: ReactNode; className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      aria-hidden="true"
      focusable="false"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
    >
      {children}
    </svg>
  )
}

/** MonitorIcon stands for the system's choice. */
export function MonitorIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <rect x="3" y="4" width="18" height="12" rx="2" />
      <path d="M8 20h8M12 16v4" />
    </Icon>
  )
}

/** SearchIcon is a magnifying glass. */
export function SearchIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-3.5-3.5" />
    </Icon>
  )
}

/** MoonIcon stands for dark. */
export function MoonIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <path d="M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5Z" />
    </Icon>
  )
}

/** SunIcon stands for light. */
export function SunIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2.5v2M12 19.5v2M2.5 12h2M19.5 12h2M5.3 5.3l1.4 1.4M17.3 17.3l1.4 1.4M5.3 18.7l1.4-1.4M17.3 6.7l1.4-1.4" />
    </Icon>
  )
}

/** SlidersIcon stands for the reader's settings. */
export function SlidersIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <path d="M4 6h9M17 6h3M4 12h3M11 12h9M4 18h11M19 18h1" />
      <circle cx="15" cy="6" r="2" />
      <circle cx="9" cy="12" r="2" />
      <circle cx="17" cy="18" r="2" />
    </Icon>
  )
}

/** PresentIcon stands for the presentation page: a screen on its stand, for a tablet, a desk or a wall. */
export function PresentIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <rect x="3" y="4" width="18" height="12" rx="1.5" />
      <path d="M12 16v4M8 20h8M7 12l3-3 2 2 4-4" />
    </Icon>
  )
}

/** BookIcon stands for the guide. */
export function BookIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <path d="M12 6.5C10.5 5 8.3 4.5 4 4.5v13c4.3 0 6.5.5 8 2 1.5-1.5 3.7-2 8-2v-13c-4.3 0-6.5.5-8 2Z" />
      <path d="M12 6.5v13" />
    </Icon>
  )
}

/** PanelRightIcon stands for a side panel at the right: folded (the chevron points to open it) or open (it points to fold it). */
export function PanelRightIcon({ open, className }: { open: boolean; className?: string }) {
  return (
    <Icon className={className}>
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="M15 4v16" />
      <path d={open ? "M8 10l2 2-2 2" : "M10 10l-2 2 2 2"} />
    </Icon>
  )
}

/** CheckIcon marks the chosen item of a menu. */
export function CheckIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <path d="M5 12.5l4.5 4.5L19 7.5" />
    </Icon>
  )
}

/** ChevronDownIcon says a button opens a menu. */
export function ChevronDownIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <path d="M7 10l5 5 5-5" />
    </Icon>
  )
}

/** ShieldIcon stands for the operators' Admin page. */
export function ShieldIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <path d="M12 3 5 6v5.5c0 4.2 2.9 7.9 7 9.5 4.1-1.6 7-5.3 7-9.5V6Z" />
    </Icon>
  )
}

/** LogInIcon stands for signing in. */
export function LogInIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <path d="M14 4h4a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-4M10 16l4-4-4-4M14 12H4" />
    </Icon>
  )
}

/** LogOutIcon stands for signing out. */
export function LogOutIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <path d="M10 4H6a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h4M16 16l4-4-4-4M20 12H10" />
    </Icon>
  )
}

/** SyncOffIcon stands for a repository the portal cannot update: its two arrows, struck through. */
export function SyncOffIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <path d="M20 11a8 8 0 0 0-13.7-4.7L4 8.5M4 13a8 8 0 0 0 13.7 4.7L20 15.5M4 4v4.5h4.5M20 20v-4.5h-4.5M3 3l18 18" />
    </Icon>
  )
}

/** ArchiveIcon stands for a dormant repository: archive it or revive it. */
export function ArchiveIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <rect x="3" y="4" width="18" height="4" rx="1" />
      <path d="M5 8v10a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8M10 12h4" />
    </Icon>
  )
}

/** HourglassIcon stands for a quiet repository: time passing without a change. */
export function HourglassIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <path d="M6 3h12M6 21h12M7 3c0 4.5 5 5.5 5 9s-5 4.5-5 9M17 3c0 4.5-5 5.5-5 9s5 4.5 5 9" />
    </Icon>
  )
}

/** TagIcon stands for releases: a tag with a dot for unreleased changes. */
export function TagIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <path d="M3 12V4a1 1 0 0 1 1-1h8l9 9-9 9Z" />
      <circle cx="8" cy="8" r="1.5" />
    </Icon>
  )
}

/** ScaleIcon stands for the licence: the scales of the law. */
export function ScaleIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <path d="M12 3v18M7 21h10M5 7h14M5 7l-3 7a3 3 0 0 0 6 0ZM19 7l-3 7a3 3 0 0 0 6 0Z" />
    </Icon>
  )
}

/** InboxIcon stands for a backlog of open issues and pull requests. */
export function InboxIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <path d="M3 13h5l1.5 3h5l1.5-3h5M5 5h14l2 8v6a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1v-6Z" />
    </Icon>
  )
}

/** FlagIcon stands for a finding: a kind of finding the portal has no drawing of, and the findings in all. */
export function FlagIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <path d="M5 21V4M5 4h11l-2 4 2 4H5" />
    </Icon>
  )
}

// The kinds of work, each a commit's badge (CommitIcon).
const person = (
  <>
    <circle cx="12" cy="8" r="3.5" />
    <path d="M5 20a7 7 0 0 1 14 0" />
  </>
)
const sparkles = (
  <path d="M10 3l1.6 4.4L16 9l-4.4 1.6L10 15l-1.6-4.4L4 9l4.4-1.6ZM18 14l.8 2.2L21 17l-2.2.8L18 20l-.8-2.2L15 17l2.2-.8Z" />
)
const bot = (
  <>
    <rect x="4" y="8" width="16" height="11" rx="2" />
    <path d="M12 4v4M9 13h.01M15 13h.01M2 13v2M22 13v2" />
  </>
)
const badges = { hand: person, ai: sparkles, automation: bot } as const

/**
 * CommitIcon stands for commits of one kind of work: a commit on its line (the history's own mark) with the kind as a badge at its
 * top right, so a count beside it reads as commits, not as people or bots.
 */
export function CommitIcon({ kind, className }: { kind: keyof typeof badges; className?: string }) {
  return (
    <Icon className={className}>
      <circle cx="6.5" cy="17.5" r="3" />
      <path d="M1 17.5h2.5M9.5 17.5H13" />
      <g transform="translate(9 0) scale(0.625)" strokeWidth="2.9">
        {badges[kind]}
      </g>
    </Icon>
  )
}

/** PulseIcon stands for activity: repositories changed in the window. */
export function PulseIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <path d="M3 12h4l2-6 4 12 2-6h6" />
    </Icon>
  )
}

const cloud = "M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z"

/** CloudCheckIcon stands for the portal's copy of GitHub, current. */
export function CloudCheckIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <path d={cloud} />
      <path d="m9.5 14 2 2 3.5-3.5" />
    </Icon>
  )
}

/** CloudOffIcon stands for the portal's copy of GitHub, failing to update. */
export function CloudOffIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <path d={cloud} />
      <path d="M3 3l18 18" />
    </Icon>
  )
}

/** GridIcon stands for the products: the fleet's groups of repositories. */
export function GridIcon({ className }: { className?: string }) {
  return (
    <Icon className={className}>
      <rect x="4" y="4" width="6.5" height="6.5" rx="1.5" />
      <rect x="13.5" y="4" width="6.5" height="6.5" rx="1.5" />
      <rect x="4" y="13.5" width="6.5" height="6.5" rx="1.5" />
      <rect x="13.5" y="13.5" width="6.5" height="6.5" rx="1.5" />
    </Icon>
  )
}
