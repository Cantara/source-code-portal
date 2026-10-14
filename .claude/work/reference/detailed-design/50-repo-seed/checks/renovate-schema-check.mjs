import { createRequire } from "node:module"
import { readFileSync, readdirSync } from "node:fs"
const [, , webDir, schemaPath, cfgPath] = process.argv
const store = `${webDir}/node_modules/.pnpm`
const dir = readdirSync(store).find((d) => /^ajv@8/.test(d))
const require = createRequire(`${store}/${dir}/node_modules/ajv/`)
const Ajv = require("ajv").default ?? require("ajv")
const ajv = new Ajv({ strict: false, allErrors: true, validateFormats: false, logger: false })
const validate = ajv.compile(JSON.parse(readFileSync(schemaPath, "utf8")))
const ok = validate(JSON.parse(readFileSync(cfgPath, "utf8")))
console.log(ok ? "renovate.json valid against renovate-schema.json" : JSON.stringify(validate.errors.slice(0, 10), null, 1))
process.exit(ok ? 0 : 1)
