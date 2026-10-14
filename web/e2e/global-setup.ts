// Waits until the fake organisation is fully synced: three fresh repositories and the documents of Whydah-Core.
export default async function globalSetup() {
  const base = "http://127.0.0.1:9190/api/v1"
  const deadline = Date.now() + 60_000
  for (;;) {
    try {
      const d = await (await fetch(`${base}/dashboard`)).json()
      const docs = await fetch(`${base}/repos/acme/whydah-core/docs`)
      const release = await (await fetch("http://127.0.0.1:9190/badges/repos/acme/whydah-core/release.svg")).text()
      if (d.freshness?.fresh === 3 && docs.ok && (await docs.json()).docs.length === 2 && release.includes("v1.2.0")) return
    } catch {
      // not up yet
    }
    if (Date.now() > deadline) throw new Error("the portal did not finish syncing the fake organisation within 60 s")
    await new Promise((r) => setTimeout(r, 250))
  }
}
