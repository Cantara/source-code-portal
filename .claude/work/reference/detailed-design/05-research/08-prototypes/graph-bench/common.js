export async function load(){ return (await fetch('./graph.json')).json(); }
export const raf=()=>new Promise(r=>requestAnimationFrame(()=>r()));
export async function frames(step,n=90){ const t=[]; let last=performance.now(); for(let i=0;i<n;i++){ step(i); await raf(); const now=performance.now(); t.push(now-last); last=now; } t.sort((a,b)=>a-b); const avg=t.reduce((a,b)=>a+b,0)/t.length; return {avgMs:+avg.toFixed(1), p95Ms:+t[Math.floor(t.length*0.95)].toFixed(1), maxMs:+t[t.length-1].toFixed(1)}; }
export function report(r){ window.__result=r; document.title='done'; }
