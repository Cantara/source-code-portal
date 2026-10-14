import Graph from 'graphology'; import Sigma from 'sigma';
const g=new Graph(); g.addNode('a',{x:0,y:0,size:5}); window.s=new Sigma(g,document.getElementById('r'));
