import {build} from 'vite'; import react from '@vitejs/plugin-react'; import fs from 'node:fs';
await build({root:'gb',configFile:false,logLevel:'warn',base:'./',plugins:[react()],publicDir:false,build:{outDir:'../out/gb',emptyOutDir:true,rollupOptions:{input:Object.fromEntries(['sigma','sigma3','cyto','xy','canvas','svg'].map(n=>[n,`gb/${n}.html`]))}}});
fs.copyFileSync('gb/graph.json','out/gb/graph.json');
