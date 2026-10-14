import { describe, expect, it } from "vitest"
import { docConfig, docInstall, docReadme } from "@/test/fixtures"
import { docIdFromSplat, docSplat, resolveDocId, splatFromDocId, stripDocExt } from "./docId"
import { buildLinkIndex, decideClick, type ClickInfo } from "./links"
import { buildDocTree, docsEntry, filterDocs, flattenDocs, sectionsContaining } from "./tree"

describe("doc ids", () => {
  it("round trips and refuses unsafe splats", () => {
    expect(docIdFromSplat("docs/install.md")).toBe("docs~install.md"); expect(splatFromDocId("docs~install.md")).toBe("docs/install.md")
    for (const bad of ["", "..", "docs/../x", "docs//x", "docs/.", "docs/\u0000x", "docs/\ud800x", "a/".padEnd(2000, "b")]) expect(docIdFromSplat(bad)).toBeNull()
  })
  it("follows the contract's DocId encoding for names outside [A-Za-z0-9._-] (V-04)", () => {
    // the example of the contract: `docs/Release Notes.md` becomes `docs~Release%20Notes.md`
    expect(docIdFromSplat("docs/Release Notes.md")).toBe("docs~Release%20Notes.md")
    expect(docIdFromSplat("docs/\u00d8konomi.md")).toBe("docs~%C3%98konomi.md")        // UTF-8 bytes, upper-case hex
    expect(docIdFromSplat("a~b")).toBe("a%7Eb"); expect(docIdFromSplat("100%.md")).toBe("100%25.md") // a literal ~ and % are escaped: injective
    for (const p of ["docs/Release Notes.md", "docs/\u00d8konomi.md", "a~b", "100%.md", "docs/\u65e5\u672c\u8a9e/\ud83d\ude00.md", "x/y/z"]) expect(splatFromDocId(docIdFromSplat(p) ?? "")).toBe(p)
    expect(splatFromDocId("docs~bad%2")).toBeNull(); expect(splatFromDocId("docs~%ff")).toBeNull() // malformed escape, invalid UTF-8
  })
  it("the index wins over the path rule", () => {
    expect(resolveDocId("README.md", [docReadme, docInstall])).toBe("readme")
    expect(resolveDocId("docs/install.md", [docReadme, docInstall])).toBe("docs~install.md")
    expect(resolveDocId("docs/other.md", [docReadme])).toBe("docs~other.md")
  })
  it("the UI splat has no document extension; both forms resolve to the same id", () => {
    expect(docSplat(docInstall)).toBe("docs/install"); expect(stripDocExt("a/B.MD")).toBe("a/B"); expect(stripDocExt("a/b.adoc")).toBe("a/b"); expect(stripDocExt("a/b.txt")).toBe("a/b.txt")
    expect(resolveDocId("docs/install", [docReadme, docInstall])).toBe("docs~install.md")
    expect(resolveDocId("docs/install.md", [docReadme, docInstall])).toBe("docs~install.md")
  })
})

describe("link click policy", () => {
  const idx = buildLinkIndex([
    { text: "Configuration", href: "/repos/billing-service/docs/docs/config.md", kind: "doc" }, { text: "Top", href: "#doc-installation", kind: "anchor" },
    { text: "GitHub", href: "https://github.com/acme/x", kind: "external" }, { text: "file", href: "https://github.com/acme/x/blob/abc/src/a.go", kind: "repo-file" },
    { text: "dup", href: "/repos/billing-service/docs/docs/config.md", kind: "external" },
  ])
  const plain: ClickInfo = { button: 0, ctrl: false, meta: false, shift: false, alt: false, target: null, download: false }
  it("routes doc links and anchors, leaves everything else to the browser", () => {
    expect(decideClick("/repos/billing-service/docs/docs/config.md", plain, idx)).toEqual({ action: "navigate", href: "/repos/billing-service/docs/docs/config.md" })
    expect(decideClick("#doc-installation", plain, idx)).toEqual({ action: "scroll", id: "doc-installation", rawId: "doc-installation" })
    expect(decideClick("https://github.com/acme/x", plain, idx)).toEqual({ action: "native" })
    expect(decideClick("//evil.example/x", plain, idx)).toEqual({ action: "native" })
    expect(decideClick("/not/in/the/index", plain, idx)).toEqual({ action: "native" })
    expect(decideClick(null, plain, idx)).toEqual({ action: "native" })
  })
  it("a malformed percent escape in an anchor never throws (V-01): the raw id is used", () => {
    const i2 = buildLinkIndex([{ text: "x", href: "#100%", kind: "anchor" }, { text: "y", href: "#doc-%C3%85", kind: "anchor" }])
    expect(decideClick("#100%", plain, i2)).toEqual({ action: "scroll", id: "100%", rawId: "100%" })
    expect(decideClick("#doc-%C3%85", plain, i2)).toEqual({ action: "scroll", id: "doc-\u00c5", rawId: "doc-%C3%85" })
  })
  it("modifier keys, middle click, target and download keep the browser default", () => {
    const h = "/repos/billing-service/docs/docs/config.md"
    for (const o of [{ ctrl: true }, { meta: true }, { shift: true }, { alt: true }, { button: 1 }, { target: "_blank" }, { download: true }]) expect(decideClick(h, { ...plain, ...o }, idx)).toEqual({ action: "native" })
    expect(decideClick(h, { ...plain, target: "_self" }, idx).action).toBe("navigate")
  })
})

describe("doc tree", () => {
  const entries = [docReadme, docInstall, docConfig]
  it("nests by section titles in API order", () => {
    const t = buildDocTree(entries)
    expect(t.map((n) => n.kind)).toEqual(["doc", "section"])
    const g = t[1]; if (g?.kind !== "section") throw new Error("section expected")
    expect(g.title).toBe("Guides"); expect(g.children.map((c) => c.kind)).toEqual(["doc", "section"])
    expect(flattenDocs(t).map((e) => e.id)).toEqual(["readme", "docs~install.md", "docs~config.md"])
    expect(sectionsContaining(entries, "docs~config.md")).toEqual([["Guides"], ["Guides", "Advanced"]])
    expect(sectionsContaining(entries, "nope")).toEqual([])
  })
  it("moves the README first without re-sorting the rest; the Documentation tab targets the first non-README document (V-14)", () => {
    const shuffled = [docInstall, docReadme, docConfig]
    expect(flattenDocs(buildDocTree(shuffled)).map((e) => e.id)).toEqual(["readme", "docs~install.md", "docs~config.md"])
    expect(docsEntry(shuffled)?.id).toBe("docs~install.md")
    expect(docsEntry([docReadme])).toBeUndefined(); expect(docsEntry([])).toBeUndefined()
  })
  it("filters on title, description, tags, path", () => {
    expect(filterDocs(entries, "setup").map((e) => e.id)).toEqual(["docs~install.md", "docs~config.md"])
    expect(filterDocs(entries, "billing service").map((e) => e.id)).toEqual(["readme"])
    expect(filterDocs(entries, "")).toHaveLength(3)
  })
})
