import "@testing-library/jest-dom/vitest"
import { cleanup } from "@testing-library/react"
import { afterEach } from "vitest"

// jsdom has no layout; the router calls scrollTo on navigation.
window.scrollTo = () => {}

afterEach(cleanup)
