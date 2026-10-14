// Per-viewer conveniences only (architecture 12.3). theme-init.js (public/) hard-codes the first two; a unit test keeps them equal.
export const STORAGE = {
  theme: "scp-theme",
  themeDefault: "scp-theme-default",
  announcementDismissed: "scp-announcement-dismissed",
  reloadGuard: "scp-reload-guard",
} as const
