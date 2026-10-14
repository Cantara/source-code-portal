import http from "node:http"; import fs from "node:fs"
import { chromium } from "playwright-core"
import { fileURLToPath } from "node:url"
const dir = fileURLToPath(new URL(".", import.meta.url))
const srv = http.createServer((req, res) => {
  if (req.url === "/") { res.writeHead(200, { "content-type": "text/html" }); res.end(`<!doctype html><meta charset=utf-8><style>#c{width:900px;height:600px}</style><div id=c></div><script type=module src=/app.js></script>`); return }
  if (req.url === "/app.js") { res.writeHead(200, { "content-type": "text/javascript" }); res.end(fs.readFileSync(dir + "/app.js")); return }
  res.writeHead(404); res.end()
}).listen(4740)
const b = await chromium.launch({ executablePath: process.env.CHROME ?? undefined, args: ["--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--ignore-gpu-blocklist"] })
const p = await b.newPage()
const logs = []; p.on("console", m => logs.push(m.type()+": "+m.text())); p.on("pageerror", e => logs.push("pageerror "+e.message))
await p.goto("http://127.0.0.1:4740/")
await p.waitForFunction(() => window.__result !== undefined, null, { timeout: 60000 })
console.log(JSON.stringify(await p.evaluate(() => window.__result), null, 1)); console.log(logs.slice(0,5))
await b.close(); srv.close()
