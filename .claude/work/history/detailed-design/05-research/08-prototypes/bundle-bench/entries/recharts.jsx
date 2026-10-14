import {createRoot} from 'react-dom/client'; import React from 'react';
import {BarChart,Bar,XAxis,YAxis,Tooltip,ResponsiveContainer} from 'recharts';
createRoot(document.getElementById('r')).render(<ResponsiveContainer><BarChart data={[{a:1}]}><XAxis dataKey="a"/><YAxis/><Tooltip/><Bar dataKey="a"/></BarChart></ResponsiveContainer>);
