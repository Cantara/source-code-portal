import {build} from 'vite'; import react from '@vitejs/plugin-react'; import fs from 'node:fs'; import path from 'node:path'; import zlib from 'node:zlib';
const names=process.argv.slice(2).length?process.argv.slice(2):fs.readdirSync('entries').map(f=>f.replace(/\.jsx$/,''));
const out=[];
for(const n of names){
  const dir=`out/${n}`; fs.rmSync(dir,{recursive:true,force:true});
  await build({logLevel:'error',configFile:false,plugins:[react()],define:{'process.env.NODE_ENV':'"production"'},build:{outDir:dir,emptyOutDir:true,lib:false,minify:true,rollupOptions:{input:`entries/${n}.jsx`,output:{entryFileNames:'[name].js',chunkFileNames:'[name].js',inlineDynamicImports:true}}}}).catch(e=>console.log(n,'ERR',e.message.slice(0,200)));
  let raw=0,gz=0,br=0; const walk=d=>{for(const f of fs.readdirSync(d,{withFileTypes:true})){const p=path.join(d,f.name); if(f.isDirectory())walk(p); else if(p.endsWith('.js')){const b=fs.readFileSync(p);raw+=b.length;gz+=zlib.gzipSync(b,{level:9}).length;br+=zlib.brotliCompressSync(b).length;}}};
  if(fs.existsSync(dir))walk(dir);
  out.push([n,(raw/1024).toFixed(1),(gz/1024).toFixed(1),(br/1024).toFixed(1)]);
}
console.log('entry raw_KB gzip_KB brotli_KB');for(const o of out)console.log(o.join(' '));
