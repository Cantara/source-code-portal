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
