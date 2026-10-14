import http from "node:http"; import fs from "node:fs"
const CSP = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; require-trusted-types-for 'script'; trusted-types scp-html"
const files = { "/app.js": ["app.js", "text/javascript"], "/assets/worker.js": ["worker.js", "text/javascript"], "/tokens.css": ["tokens.css", "text/css"], "/page.css": ["page.css", "text/css"] }
http.createServer((req, res) => {
  const u = new URL(req.url, "http://x")
  if (u.pathname === "/" ) { res.writeHead(200, { "content-type": "text/html", "content-security-policy": CSP }); res.end(`<!doctype html><html><head><meta charset=utf-8><link rel=stylesheet href=/tokens.css><link rel=stylesheet href=/page.css><title>t</title></head><body><div id=c></div><script type=module src=/app.js></script></body></html>`); return }
  const f = files[u.pathname]; if (!f) { res.writeHead(404); res.end(); return }
  res.writeHead(200, { "content-type": f[1], "x-content-type-options": "nosniff" }); res.end(fs.readFileSync(new URL(f[0], import.meta.url)))
}).listen(4731, () => console.log("up 4731"))
