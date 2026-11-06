// Waits until the fake organisation is fully synced: four fresh repositories (the archived one too), the documents of Whydah-Core, its release, and every
// job a spec reads (the 5 open pull requests and the 4 open issues of cmd/scp/e2e_test.go), so no test meets a half-synced portal.
export default async function globalSetup() {
  const base = "http://127.0.0.1:9190/api/v1"
  const deadline = Date.now() + 60_000
  for (;;) {
    try {
      const d = await (await fetch(`${base}/dashboard`)).json()
      const docs = await fetch(`${base}/repos/acme/whydah-core/docs`)
      const release = await (await fetch("http://127.0.0.1:9190/badges/repos/acme/whydah-core/release.svg")).text()
      const pulls = await (await fetch(`${base}/pulls`)).json()
      const issues = await (await fetch(`${base}/issues`)).json()
      if (
        d.freshness?.fresh === 4 &&
        docs.ok &&
        (await docs.json()).docs.length === 2 &&
        release.includes("v1.2.0") &&
        pulls.counts?.open === 5 &&
        issues.counts?.open === 4
      )
        return
    } catch {
      // not up yet
    }
    if (Date.now() > deadline) throw new Error("the portal did not finish syncing the fake organisation within 60 s")
    await new Promise((r) => setTimeout(r, 250))
  }
}
