import mermaid from 'mermaid';
mermaid.initialize({startOnLoad:false, securityLevel:'strict', theme:'neutral'});
addEventListener('message', async (e)=>{ const {id,src}=e.data||{}; try{ const {svg}=await mermaid.render('d'+id,src); parent.postMessage({id,svg},'*'); }catch(err){ parent.postMessage({id,error:String(err)},'*'); } });
parent.postMessage({ready:true},'*');
