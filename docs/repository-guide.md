# Repository guide

The portal shows every public repository of the configured organisations and user accounts. This page is for repository owners: what the portal reads
from a repository and how to describe it.

## What the portal reads

| Shown | Read from |
|---|---|
| Name, description, homepage, topics, licence, stars, open issues and pull requests, last push | the repository on GitHub |
| README | a `README` file (any case and extension, `README.md` preferred) in `.github/`, else the root, else `docs/` |
| Documentation | up to 50 Markdown files (`.md`, `.markdown`) under `docs/`, in path order |
| Latest commits | the 30 newest commits of the default branch |
| Release badge | the latest GitHub release |
| Title, owners, lifecycle, tags, links | the optional `scp.yaml` descriptor (below) |
| Technology and dependencies | up to 30 package manifests: `go.mod`, `package.json` and `pom.xml`, at most four directories deep, outside `node_modules`, `vendor`, `test`, `tests`, `testdata`, `example`, `examples`, `fixtures` and hidden directories |

A document's front matter may set `title`, `order` (a number; lower comes first) and `hidden: true` (rendered and linkable, but not
listed):

```markdown
---
title: Installing
order: 1
---
```

Documents render as GitHub-flavoured Markdown: tables, task lists, footnotes, alerts (`> [!NOTE]`, `[!TIP]`, `[!IMPORTANT]`,
`[!WARNING]`, `[!CAUTION]`) and highlighted code blocks. Relative links to other documents stay in the portal. Images, relative
ones and `https` ones from other hosts, are served by the portal: PNG, JPEG, GIF, WebP, BMP, ICO, AVIF and SVG up to 5 MiB. Images outside
the root, `.github/` and `docs/` are found in up to five other top-level directories. Scripts, styles, forms and embedded frames are removed. A very large document shows its first part with a
link to the full file on GitHub.

Manifests are read, never run: the portal shows what they declare. From `go.mod` the module path, the `go` and `toolchain` versions,
`require` (`// indirect` shown as indirect) and `tool`; from `package.json` the name, version, `engines.node`, `packageManager` (or the
lockfile beside it) and the four dependency fields; from `pom.xml` the coordinates (inherited from `parent`), the parent, the
dependencies with their scopes, BOM imports and the Java version from `maven.compiler.release` (or `target`, `source`, `java.version`),
with `${property}` references replaced from the POM's own properties. A dependency whose name another catalogued repository declares
(Go module path, npm package name, Maven `groupId:artifactId`) links the two repositories: each page lists what it uses and what uses it.
A file over 512 KiB, or one that does not parse, is listed with the problem.

Changes show within seconds when the portal's webhook is set up, otherwise after the next scheduled check (a push is noticed within
the listing interval, five minutes by default).

## Health

Every repository gets a health score from 0 to 100, so the owner of a product sees what needs attention. It starts at 100 and loses
points for each finding, and the portal shows the findings beside the score:

| Finding | Points | When |
|---|---|---|
| SCP cannot update it | 25 | the portal failed to read the repository or its commits three times in a row |
| dormant | 30 | no change by a person for 2 years: archive it or revive it |
| quiet | 10 | no change by a person for 6 months |
| unreleased changes | 20 | commits landed after the latest release, and that release is older than 90 days |
| no licence | 20 | a public repository where GitHub detects no licence |
| open issues and pull requests | 10 or 20 | more than 20, or more than 50 |

Commits by automation do not count as change: authors ending in `[bot]` (Renovate, Dependabot, GitHub Actions) and
`[maven-release-plugin]` commits. They do count as unreleased changes. 80 and above is good, 50 and above is to watch, below 50 needs
attention. Archived repositories and those with `lifecycle: deprecated`
in their descriptor have no score.

A group's health weighs its repositories: one changed in the last 6 months counts twice, the group's default repository three times
more. A group with a repository that needs attention is never shown as good.

## The `scp.yaml` descriptor

Place `scp.yaml` (or `scp.yml`, not both) in `.github/` or at the repository root; `.github/` wins when both exist. Every field is
optional:

```yaml
schema: 1
title: Whydah Core                # 1 to 120 characters; the page title
description: Identity and access  # 1 to 500 characters; replaces the GitHub description on the page
lifecycle: production             # experimental | production | deprecated
owners: ["@cantara/iam", "@ann"]  # @user or @org/team, at most 20
tags: [java, identity]            # lower-case letters, digits and -, at most 20
links:                            # at most 20; title 1 to 80 characters, url https
  - { title: Runbook, url: "https://runbook.example.org/core" }
```

The descriptor changes what the page displays; group membership and access come from the portal's configuration. A field with a wrong
value is ignored and listed on the repository page as a problem with its location in the file, so a mistake never hides the rest. Files
over 64 KiB, YAML anchors and aliases are refused as a whole.
