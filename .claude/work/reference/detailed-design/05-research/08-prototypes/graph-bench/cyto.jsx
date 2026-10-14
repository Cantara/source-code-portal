import cytoscape from 'cytoscape'; import {load,raf,frames,report} from './common.js';
const d=await load(); const t0=performance.now();
const cy=cytoscape({container:document.getElementById('r'),elements:[...d.nodes.map(n=>({data:{id:n.id,label:n.label,cluster:n.cluster},position:{x:n.x,y:n.y}})),...d.edges.map((e,i)=>({data:{id:'e'+i,source:e.s,target:e.t}}))],layout:{name:'preset'},style:[{selector:'node',style:{width:6,height:6,'background-color':'#2563eb'}},{selector:'edge',style:{width:1,'line-color':'#ccc','curve-style':'haystack'}}]});
await raf(); await raf(); const mount=performance.now()-t0;
const f=await frames(i=>cy.pan({x:i*3,y:0}));
report({lib:'cytoscape(haystack,no labels)',mountMs:Math.round(mount),...f});
