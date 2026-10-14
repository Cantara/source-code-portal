const f=document.getElementById('f'); const def=`flowchart LR\n  A[Repo A] --> B[Repo B]\n  B --> C{Lib}\n  classDef x fill:#f9f,stroke:#333;\n  class A x`;
addEventListener('message',(e)=>{ if(e.source!==f.contentWindow) return; const d=e.data; if(d.ready){ f.contentWindow.postMessage({id:1,src:def},'*'); }
 else if(d.svg){ window.__svg=d.svg; const img=new Image(); img.alt='diagram'; img.onload=()=>{window.__done={w:img.naturalWidth,h:img.naturalHeight}}; img.src='data:image/svg+xml;charset=utf-8,'+encodeURIComponent(d.svg); document.getElementById('out').appendChild(img);} });
