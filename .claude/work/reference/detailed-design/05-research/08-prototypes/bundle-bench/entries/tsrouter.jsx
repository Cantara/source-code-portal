import {createRoot} from 'react-dom/client'; import React from 'react';
import {createRouter,createRootRoute,createRoute,RouterProvider,Link,Outlet} from '@tanstack/react-router';
const root=createRootRoute({component:()=><Outlet/>}); const idx=createRoute({getParentRoute:()=>root,path:'/',validateSearch:(s)=>s,component:()=><Link to="/">x</Link>});
const router=createRouter({routeTree:root.addChildren([idx])});
createRoot(document.getElementById('r')).render(<RouterProvider router={router}/>);
