import { http, HttpResponse } from "msw"
import { setupServer } from "msw/node"
import createClient from "openapi-fetch"
import { afterAll, beforeAll, expect, test } from "vitest"
interface paths { "/api/v1/groups": { get: { responses: { 200: { content: { "application/json": { items: { id: string }[] } } } } } } }
const server = setupServer(http.get("http://localhost/api/v1/groups", () => HttpResponse.json({ items: [{ id: "identity" }] }, { headers: { ETag: '"v1"' } })))
beforeAll(() => server.listen()); afterAll(() => server.close())
test("msw 3 + openapi-fetch", async () => {
  const c = createClient<paths>({ baseUrl: "http://localhost" })
  const { data, response } = await c.GET("/api/v1/groups")
  expect(data?.items[0]?.id).toBe("identity"); expect(response.headers.get("etag")).toBe('"v1"')
})
