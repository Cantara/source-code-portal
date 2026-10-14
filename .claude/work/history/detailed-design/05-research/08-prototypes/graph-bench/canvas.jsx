import {zoom,zoomIdentity} from 'd3-zoom'; import {select} from 'd3-selection'; import {load,raf,frames,report} from './common.js';
const d=await load(); const t0=performance.now();
const c=document.createElement('canvas'); c.width=1200;c.height=800; document.getElementById('r').appendChild(c); const ctx=c.getContext('2d'); const idx=new Map(d.nodes.map(n=>[n.id,n]));
function draw(t){ ctx.save(); ctx.clearRect(0,0,1200,800); ctx.translate(t.x,t.y); ctx.scale(t.k,t.k); ctx.strokeStyle='#ccc'; ctx.beginPath(); for(const e of d.edges){const a=idx.get(e.s),b=idx.get(e.t); ctx.moveTo(a.x,a.y); ctx.lineTo(b.x,b.y);} ctx.stroke(); ctx.fillStyle='#2563eb'; for(const n of d.nodes){ctx.beginPath();ctx.arc(n.x,n.y,3,0,6.283);ctx.fill();} ctx.restore(); }
draw({x:600,y:400,k:1}); await raf(); await raf(); const mount=performance.now()-t0;
const f=await frames(i=>draw({x:600-i*3,y:400,k:1+i*0.005}));
report({lib:'d3 + canvas2d (hand-rolled)',mountMs:Math.round(mount),...f});
