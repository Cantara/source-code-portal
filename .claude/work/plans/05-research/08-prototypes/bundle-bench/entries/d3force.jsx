import {forceSimulation,forceLink,forceManyBody,forceCenter} from 'd3-force'; import {zoom} from 'd3-zoom'; import {select} from 'd3-selection';
const s=forceSimulation([{}]).force('l',forceLink([])).force('c',forceManyBody()).force('x',forceCenter()); window.s=s; select('#r').call(zoom());
