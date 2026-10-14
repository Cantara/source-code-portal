import {createRoot} from 'react-dom/client'; import React from 'react';
import {ReactFlow,Background,Controls,MiniMap} from '@xyflow/react';
createRoot(document.getElementById('r')).render(<ReactFlow nodes={[{id:'a',position:{x:0,y:0},data:{label:'a'}}]} edges={[]}><Background/><Controls/><MiniMap/></ReactFlow>);
