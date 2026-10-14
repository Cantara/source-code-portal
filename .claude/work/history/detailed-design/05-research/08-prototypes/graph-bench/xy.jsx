import React from 'react'; import {createRoot} from 'react-dom/client'; import {ReactFlow,ReactFlowProvider,useReactFlow} from '@xyflow/react'; import '@xyflow/react/dist/style.css'; import {load,raf,frames,report} from './common.js';
const d=await load(); const t0=performance.now();
const nodes=d.nodes.map(n=>({id:n.id,position:{x:n.x*3,y:n.y*3},data:{label:n.label},style:{width:60,fontSize:8,padding:2}})); const edges=d.edges.map((e,i)=>({id:'e'+i,source:e.s,target:e.t}));
function Runner(){ const rf=useReactFlow(); React.useEffect(()=>{ (async()=>{ await raf(); await raf(); const mount=performance.now()-t0; const f=await frames(i=>rf.setViewport({x:-i*3,y:0,zoom:0.5})); report({lib:'xyflow'+(window.__vis?'(onlyRenderVisible)':'(all nodes)'),mountMs:Math.round(mount),...f}); })(); },[]); return null; }
window.__vis=location.search.includes('vis');
createRoot(document.getElementById('r')).render(<div style={{width:1200,height:800}}><ReactFlowProvider><ReactFlow nodes={nodes} edges={edges} fitView onlyRenderVisibleElements={window.__vis} nodesDraggable={false}><Runner/></ReactFlow></ReactFlowProvider></div>);
