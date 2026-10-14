import {createRoot} from 'react-dom/client'; import React from 'react';
import {useVirtualizer} from '@tanstack/react-virtual';
function V(){const r=React.useRef();const v=useVirtualizer({count:1000,getScrollElement:()=>r.current,estimateSize:()=>30});return <div ref={r}>{v.getVirtualItems().length}</div>}
createRoot(document.getElementById('r')).render(<V/>);
