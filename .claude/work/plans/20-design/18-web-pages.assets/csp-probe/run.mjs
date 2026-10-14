import { chromium } from "playwright"
const variant = process.argv[2] ?? ""
const b = await chromium.launch({ args: ["--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--ignore-gpu-blocklist"] })
const p = await b.newPage({ viewport: { width: 1000, height: 700 } })
const viol = [], cons = []
await p.addInitScript(() => { window.__v = []; document.addEventListener("securitypolicyviolation", (e) => window.__v.push(`${e.effectiveDirective} ${e.blockedURI} ${e.sample ?? ""}`)) })
p.on("console", (m) => { if (m.type() === "error") cons.push(m.text()) })
p.on("pageerror", (e) => cons.push("pageerror " + e.message))
const cdp = await p.context().newCDPSession(p)
if (process.argv[3] === "throttle") await cdp.send("Emulation.setCPUThrottlingRate", { rate: 6 })
await p.goto("http://127.0.0.1:4731/" + variant)
await p.waitForFunction(() => window.__result !== undefined, null, { timeout: 60000 })
const res = await p.evaluate(() => ({ result: window.__result, violations: window.__v }))
console.log(JSON.stringify({ variant, throttle: process.argv[3] ?? "none", ...res, console: cons }, null, 1))
await b.close()
