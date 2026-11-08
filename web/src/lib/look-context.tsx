"use client"

import { createContext, useContext, type ReactNode } from "react"
import { productName, type Look } from "./look"

const Context = createContext<Look>({ name: productName })

/** LookProvider passes the look the root layout read to the client components that print the name or the logo. */
export function LookProvider({ look, children }: { look: Look | undefined; children: ReactNode }) {
  return <Context value={look ?? { name: productName }}>{children}</Context>
}

/** useLook is the deployment's look: the built-in name and no logo without one. */
export function useLook(): Look {
  return useContext(Context)
}
