# Repository guide

The portal shows every public repository of the configured organisations. This page is for repository owners: what the portal reads
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

A document's front matter may set `title`, `order` (a number; lower comes first) and `hidden: true` (rendered and linkable, but not
listed):

```markdown
---
title: Installing
order: 1
---
```

Documents render as GitHub-flavoured Markdown: tables, task lists, footnotes, alerts (`> [!NOTE]`, `[!TIP]`, `[!IMPORTANT]`,
`[!WARNING]`, `[!CAUTION]`) and highlighted code blocks. Relative links to other documents stay in the portal; relative images load from
`raw.githubusercontent.com`. Scripts, styles, forms and embedded frames are removed. A very large document shows its first part with a
link to the full file on GitHub.

Changes show within seconds when the organisation webhook is set up, otherwise after the next scheduled check (a push is noticed within
the listing interval, five minutes by default).

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
