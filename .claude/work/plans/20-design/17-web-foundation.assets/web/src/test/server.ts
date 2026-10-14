import { setupServer } from "msw/node"
import { createOpenApiHttp } from "openapi-msw"
import type { paths } from "@/lib/api/schema"

/** Handlers are typed from the generated `paths`: a wrong path, status or body shape is a compile error. */
export const http = createOpenApiHttp<paths>({ baseUrl: window.location.origin })
export const server = setupServer()
