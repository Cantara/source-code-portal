import dagre from '@dagrejs/dagre'; const g=new dagre.graphlib.Graph(); g.setGraph({}); dagre.layout(g); window.g=g;
