import Graph from 'graphology'; import Sigma from 'sigma3'; import {load,raf,frames,report} from './common.js';
const d=await load(); const t0=performance.now();
const g=new Graph(); const colors=['#e11d48','#2563eb','#16a34a','#d97706','#7c3aed'];
d.nodes.forEach(n=>g.addNode(n.id,{x:n.x,y:n.y,size:3,label:n.label,color:colors[n.cluster%5]})); d.edges.forEach((e,i)=>g.addEdgeWithKey('e'+i,e.s,e.t,{size:1,color:'#ccc'}));
const s=new Sigma(g,document.getElementById('r')); await raf(); await raf(); const mount=performance.now()-t0;
const cam=s.getCamera(); const f=await frames(i=>cam.setState({x:0.5+Math.sin(i/10)*0.1,y:0.5,ratio:1+i*0.005}));
report({lib:'sigma 3.0.3',mountMs:Math.round(mount),...f});
