import http from 'node:http'; import fs from 'node:fs'; import path from 'node:path';
const root=process.argv[2]; const port=+process.argv[3];
const MAIN="default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; object-src 'none'";
const FRAME="sandbox allow-scripts; default-src 'none'; script-src 'self'; style-src 'unsafe-inline'; img-src data:; connect-src 'none'";
const types={'.html':'text/html','.js':'text/javascript','.css':'text/css'};
http.createServer((req,res)=>{let p=new URL(req.url,'http://x').pathname; if(p==='/')p='/index.html'; let f=path.join(root,p); if(!fs.existsSync(f)){res.statusCode=404;return res.end('nf')}
 res.setHeader('Content-Type',types[path.extname(f)]||'application/octet-stream'); res.setHeader('Access-Control-Allow-Origin','*');
 if(p==='/frame.html') res.setHeader('Content-Security-Policy',FRAME); else if(p.endsWith('.html')) res.setHeader('Content-Security-Policy',MAIN);
 res.end(fs.readFileSync(f));}).listen(port);
