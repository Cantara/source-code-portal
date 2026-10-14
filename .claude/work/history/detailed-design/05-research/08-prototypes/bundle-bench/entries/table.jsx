import {createRoot} from 'react-dom/client'; import React from 'react';
import {useTable,tableFeatures,rowSortingFeature,createSortedRowModel,sortFns,createColumnHelper} from '@tanstack/react-table';
const features=tableFeatures({rowSortingFeature});
function T(){const t=useTable({features,_rowModels:{sortedRowModel:createSortedRowModel(sortFns)},columns:[],data:[]});return <div>{String(!!t)}</div>}
createRoot(document.getElementById('r')).render(<T/>);
