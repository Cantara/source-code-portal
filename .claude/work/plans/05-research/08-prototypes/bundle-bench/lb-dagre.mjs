import dagre from '@dagrejs/dagre'; import fs from 'node:fs';
const d=JSON.parse(fs.readFileSync('gb/graph.json')); const N=+process.argv[2];
const g=new dagre.graphlib.Graph(); g.setGraph({rankdir:'LR',nodesep:10,ranksep:40}); g.setDefaultEdgeLabel(()=>({}));
const keep=new Set(d.nodes.slice(0,N).map(n=>n.id)); d.nodes.slice(0,N).forEach(n=>g.setNode(n.id,{width:60,height:16}));
const num=s=>+s.slice(1); let ne=0; d.edges.forEach(e=>{ if(!keep.has(e.s)||!keep.has(e.t))return; const a=num(e.s),b=num(e.t); a<b?g.setEdge(e.s,e.t):g.setEdge(e.t,e.s); ne++;});
let t=performance.now(); dagre.layout(g); console.log('dagre nodes',N,'edges',ne,'ms',Math.round(performance.now()-t));
