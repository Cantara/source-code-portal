import {load,raf,frames,report} from './common.js';
const d=await load(); const t0=performance.now(); const NS='http://www.w3.org/2000/svg';
const svg=document.createElementNS(NS,'svg'); svg.setAttribute('width',1200); svg.setAttribute('height',800); const gEl=document.createElementNS(NS,'g'); svg.appendChild(gEl); document.getElementById('r').appendChild(svg);
const idx=new Map(d.nodes.map(n=>[n.id,n]));
for(const e of d.edges){const a=idx.get(e.s),b=idx.get(e.t);const l=document.createElementNS(NS,'line');l.setAttribute('x1',a.x);l.setAttribute('y1',a.y);l.setAttribute('x2',b.x);l.setAttribute('y2',b.y);l.setAttribute('stroke','#ccc');gEl.appendChild(l);}
for(const n of d.nodes){const c=document.createElementNS(NS,'circle');c.setAttribute('cx',n.x);c.setAttribute('cy',n.y);c.setAttribute('r',3);c.setAttribute('fill','#2563eb');gEl.appendChild(c);}
gEl.setAttribute('transform','translate(600,400)'); await raf(); await raf(); const mount=performance.now()-t0;
const f=await frames(i=>gEl.setAttribute('transform',`translate(${600-i*3},400) scale(${1+i*0.005})`));
report({lib:'plain SVG DOM (1000 circles+2500 lines)',mountMs:Math.round(mount),...f});
