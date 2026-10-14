import {createRoot} from 'react-dom/client'; import React from 'react'; import {create} from 'zustand';
const u=create(s=>({n:0})); function A(){return <div>{u(s=>s.n)}</div>} createRoot(document.getElementById('r')).render(<A/>);
