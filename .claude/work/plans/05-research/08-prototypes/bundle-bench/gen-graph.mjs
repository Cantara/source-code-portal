import {forceSimulation,forceLink,forceManyBody,forceCenter} from 'd3-force'; import fs from 'node:fs';
let seed=42; const rnd=()=>{seed=(seed*1664525+1013904223)%4294967296; return seed/4294967296;};
const N=1000,E=2500,C=20; const nodes=[...Array(N)].map((_,i)=>({id:'n'+i,cluster:i%C,label:'repo-'+i}));
const edges=new Set(); const links=[];
while(links.length<E){ const a=Math.floor(rnd()*N); let b; if(rnd()<0.8){ b=(Math.floor(rnd()*(N/C))*C)+(a%C);} else b=Math.floor(rnd()*N); if(a===b)continue; const k=a<b?a+'-'+b:b+'-'+a; if(edges.has(k))continue; edges.add(k); links.push({source:'n'+a,target:'n'+b}); }
const t0=performance.now();
const sim=forceSimulation(nodes).force('l',forceLink(links).id(d=>d.id).distance(30)).force('c',forceManyBody().strength(-30)).stop();
for(let i=0;i<300;i++)sim.tick();
const t1=performance.now();
fs.writeFileSync('gb/graph.json',JSON.stringify({nodes:nodes.map(n=>({id:n.id,label:n.label,cluster:n.cluster,x:Math.round(n.x*10)/10,y:Math.round(n.y*10)/10})),edges:links.map(l=>({s:l.source.id,t:l.target.id}))}));
console.log('d3-force 300 ticks node ms',Math.round(t1-t0),'nodes',N,'edges',links.length);
