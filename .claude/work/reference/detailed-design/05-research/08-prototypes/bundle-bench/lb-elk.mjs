import fs from 'node:fs'; const d=JSON.parse(fs.readFileSync('gb/graph.json')); const N=+process.argv[2];
const ELK=(await import('elkjs/lib/elk.bundled.js')).default; const elk=new ELK(); const keep=new Set(d.nodes.slice(0,N).map(n=>n.id)); const num=s=>+s.slice(1);
const edges=d.edges.filter(e=>keep.has(e.s)&&keep.has(e.t));
const t=performance.now(); await elk.layout({id:'r',layoutOptions:{'elk.algorithm':'layered','elk.direction':'RIGHT'},children:d.nodes.slice(0,N).map(n=>({id:n.id,width:60,height:16})),edges:edges.map((e,i)=>{const a=num(e.s),b=num(e.t);return {id:'e'+i,sources:[a<b?e.s:e.t],targets:[a<b?e.t:e.s]}})}); console.log('elk nodes',N,'edges',edges.length,'ms',Math.round(performance.now()-t));
