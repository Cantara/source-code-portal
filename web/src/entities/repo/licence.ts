/**
 * The short names of the common licences, by SPDX identifier, as people say them; another licence shows its SPDX identifier.
 * Identifiers ending in -only or -or-later name the same licence.
 */
const short: Record<string, string> = {
  "Apache-2.0": "APL2",
  "Apache-1.1": "APL1.1",
  MIT: "MIT",
  "BSD-2-Clause": "BSD2",
  "BSD-3-Clause": "BSD3",
  "GPL-2.0": "GPL2",
  "GPL-3.0": "GPL3",
  "LGPL-2.1": "LGPL2.1",
  "LGPL-3.0": "LGPL3",
  "AGPL-3.0": "AGPL3",
  "MPL-2.0": "MPL2",
  "EPL-1.0": "EPL1",
  "EPL-2.0": "EPL2",
  "CC0-1.0": "CC0",
  "CC-BY-4.0": "CC BY4",
  ISC: "ISC",
  Unlicense: "Unlicense",
  "BSL-1.0": "BSL1",
}

/** Licence is a repository's licence: its short name, its SPDX identifier and the address of its text; none without one. */
export type Licence = { short: string; spdx: string; url: string }

/** licence names spdx, the identifier GitHub detected; "" and NOASSERTION (a licence GitHub could not name) are none. */
export function licence(spdx: string): Licence | undefined {
  if (spdx === "" || spdx === "NOASSERTION") return undefined
  const base = spdx.replace(/-(only|or-later)$/, "")
  return { short: short[base] ?? spdx, spdx, url: `https://spdx.org/licenses/${encodeURIComponent(spdx)}.html` }
}
