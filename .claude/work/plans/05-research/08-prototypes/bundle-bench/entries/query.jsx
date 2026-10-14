import {createRoot} from 'react-dom/client'; import React from 'react';
import {QueryClient,QueryClientProvider,useQuery} from '@tanstack/react-query';
const qc=new QueryClient(); function A(){const q=useQuery({queryKey:['a'],queryFn:()=>fetch('/x').then(r=>r.json())});return <div>{String(q.data)}</div>}
createRoot(document.getElementById('r')).render(<QueryClientProvider client={qc}><A/></QueryClientProvider>);
