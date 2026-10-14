import { t } from "@lingui/core/macro"
import { X } from "lucide-react"
import { useState } from "react"
import type { components } from "@/lib/api/schema"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { readPersisted, writePersisted } from "@/lib/persist"
import { STORAGE } from "@/lib/storage-keys"

type Announcement = components["schemas"]["Announcement"]

/** Plain text only (no HTML). Dismissal is remembered per content hash (`id`), so a changed text shows again. */
export function AnnouncementBanner({ announcement }: { announcement: Announcement }) {
  // the id of the dismissed announcement, not a boolean: when the site query delivers a new announcement while this
  // component is mounted, the new id differs and the banner shows again without a reload
  const [dismissedId, setDismissedId] = useState(() => readPersisted(STORAGE.announcementDismissed))
  if (dismissedId === announcement.id) return null
  return (
    <Alert
      role="region"
      aria-label={t`Announcement`}
      variant={announcement.level === "critical" ? "destructive" : "default"}
      data-level={announcement.level}
    >
      <AlertDescription className="flex items-start justify-between gap-2">
        <span>{announcement.text}</span>
        {announcement.dismissible ? (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t`Dismiss announcement`}
            onClick={() => {
              writePersisted(STORAGE.announcementDismissed, announcement.id)
              setDismissedId(announcement.id)
            }}
          >
            <X aria-hidden="true" />
          </Button>
        ) : null}
      </AlertDescription>
    </Alert>
  )
}
