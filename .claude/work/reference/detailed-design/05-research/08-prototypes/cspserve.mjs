import http from 'node:http'; import fs from 'node:fs'; import path from 'node:path';
const root=process.argv[2]; const port=+process.argv[3]; const csp=process.argv[4]||"default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'";
const types={'.html':'text/html','.js':'text/javascript','.css':'text/css','.woff2':'font/woff2','.svg':'image/svg+xml','.json':'application/json'};
http.createServer((req,res)=>{let p=new URL(req.url,'http://x').pathname; let f=path.join(root,p); if(!fs.existsSync(f)||fs.statSync(f).isDirectory()) f=path.join(root,'index.html'); if(!fs.existsSync(f)){res.statusCode=404;return res.end('nf');}
 res.setHeader('Content-Type',types[path.extname(f)]||'application/octet-stream'); res.setHeader('Content-Security-Policy',csp); res.end(fs.readFileSync(f));}).listen(port,()=>console.log('up',port));
