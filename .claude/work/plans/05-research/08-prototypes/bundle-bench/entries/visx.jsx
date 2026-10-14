import {createRoot} from 'react-dom/client'; import React from 'react';
import {Bar} from '@visx/shape'; import {scaleBand,scaleLinear} from '@visx/scale'; import {AxisBottom} from '@visx/axis'; import {Group} from '@visx/group';
const x=scaleBand({domain:['a'],range:[0,100]});const y=scaleLinear({domain:[0,1],range:[100,0]});
createRoot(document.getElementById('r')).render(<svg><Group><Bar x={x('a')} y={y(1)} width={x.bandwidth()} height={10}/><AxisBottom scale={x}/></Group></svg>);
