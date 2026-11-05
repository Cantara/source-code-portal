import { describe, expect, it } from "vitest"
import { now } from "@/entities/announcement/fixture"
import { local, preview, slug, state, written } from "./announcements"

describe("the admin's announcements", () => {
  it("edit times as written and save them with the browser's offset", () => {
    expect(local("2026-11-26T18:00:00+01:00")).toBe("2026-11-26T18:00")
    expect(written("2026-11-26T18:00")).toMatch(/^2026-11-26T18:00:00[+-]\d{2}:\d{2}$/)
    expect(written("")).toBeUndefined()
  })
  it("make an id of the title and say where each stands", () => {
    expect(slug("Juleølsmaking på Rebel Makerspace 2026!")).toBe("juleolsmaking-pa-rebel-makerspace-2026")
    const base = { id: "a", title: "A", until: "2026-12-01T00:00:00Z" }
    expect(state({ ...base, published: false }, now)).toBe("Draft")
    expect(state(base, now)).toBe("Live")
    expect(state({ ...base, from: "2026-11-25T00:00:00Z" }, now)).toBe("Scheduled")
    expect(state({ ...base, until: "2026-11-01T00:00:00Z" }, now)).toBe("Ended")
    expect(preview({ id: "", title: "", ends: "2026-11-26T23:00:00+01:00" }).until).toBe("2026-11-26T23:00:00+01:00")
  })
})
