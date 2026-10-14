import * as z from 'zod/mini'; const S=z.object({a:z.string().check(z.minLength(1)),b:z.number(),c:z.array(z.object({d:z.enum(['x','y'])}))}); window.S=S; console.log(S.parse(window.x));
