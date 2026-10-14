import {build} from 'vite'; import {svelte} from '@sveltejs/vite-plugin-svelte'; import fs from 'node:fs'; import path from 'node:path'; import zlib from 'node:zlib';
await build({logLevel:'error',configFile:false,plugins:[svelte()],build:{outDir:'out/sv',emptyOutDir:true,rollupOptions:{input:'sv/main.js',output:{codeSplitting:false}}}});
let raw=0,gz=0,br=0; for(const f of fs.readdirSync('out/sv',{recursive:true})){const p=path.join('out/sv',f); if(p.endsWith('.js')){const b=fs.readFileSync(p);raw+=b.length;gz+=zlib.gzipSync(b,{level:9}).length;br+=zlib.brotliCompressSync(b).length;}}
console.log('svelte5 hello+state raw',(raw/1024).toFixed(1),'gz',(gz/1024).toFixed(1),'br',(br/1024).toFixed(1));
