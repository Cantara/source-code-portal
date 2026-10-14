import {z} from 'zod'; const S=z.object({a:z.string().min(1),b:z.number(),c:z.array(z.object({d:z.enum(['x','y'])}))}); window.S=S; console.log(S.parse(window.x));
