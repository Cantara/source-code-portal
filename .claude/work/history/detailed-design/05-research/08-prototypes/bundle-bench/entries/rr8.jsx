import {createRoot} from 'react-dom/client'; import React from 'react';
import {createBrowserRouter,RouterProvider,Link,Outlet} from 'react-router';
const router=createBrowserRouter([{path:'/',Component:()=><Link to="/">x</Link>,loader:()=>null}]);
createRoot(document.getElementById('r')).render(<RouterProvider router={router}/>);
