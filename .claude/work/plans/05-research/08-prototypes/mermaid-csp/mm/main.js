import mermaid from 'mermaid';
mermaid.initialize({startOnLoad:false, securityLevel:'strict', theme:'neutral'});
const defs={flow:`flowchart LR\n  A[Repo A] --> B[Repo B]\n  B --> C{Lib}\n  classDef x fill:#f9f,stroke:#333;\n  class A x`, seq:`sequenceDiagram\n  Alice->>Bob: Hi\n  Bob-->>Alice: Yo`};
const a=document.getElementById('a'), b=document.getElementById('b');
const {svg}=await mermaid.render('m1',defs.flow);
a.innerHTML=svg;                       // (A) direct inline SVG
const {svg:s2}=await mermaid.render('m2',defs.seq);
const img=new Image(); img.alt='diagram'; img.src='data:image/svg+xml;charset=utf-8,'+encodeURIComponent(s2); b.appendChild(img); // (B) as image
await new Promise(r=>img.onload=r);
window.__svg=[svg,s2];window.__done={imgW:img.naturalWidth,imgH:img.naturalHeight,inlineStyleEls:a.querySelectorAll('style').length,styleAttrs:a.querySelectorAll('[style]').length};
