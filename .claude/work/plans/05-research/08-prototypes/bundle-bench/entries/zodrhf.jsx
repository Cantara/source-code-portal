import {createRoot} from 'react-dom/client'; import React from 'react';
import {useForm} from 'react-hook-form'; import {z} from 'zod';
const S=z.object({a:z.string().min(1),b:z.number()}); function F(){const f=useForm();return <input {...f.register('a')} onChange={()=>S.parse({})}/>}
createRoot(document.getElementById('r')).render(<F/>);
