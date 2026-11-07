// The setup page: sign in to GitHub, choose the accounts, sort the repositories into products, start the portal. It builds its DOM
// with text nodes only (a repository name is data, never markup) and keeps the token out of the page: the server holds it.

const app = document.getElementById("app")
const STEPS = [
  ["signin", "Sign in to GitHub"],
  ["accounts", "Accounts"],
  ["products", "Products"],
  ["start", "Start"],
]

let st = null // the server's state
let step = "signin"
let error = ""
let busy = false
let timer = null
let accounts = new Set()
let listing = null // the repositories and the suggested products
let products = [] // { key, name }
let assign = {} // repository name -> product key, "" (no product) or "hide"
let nextKey = 1

function h(tag, attrs = {}, ...kids) {
  const el = document.createElement(tag)
  for (const [k, v] of Object.entries(attrs)) {
    if (v === false || v == null) continue
    if (k === "on") for (const [ev, fn] of Object.entries(v)) el.addEventListener(ev, fn)
    else if (k === "class") el.className = v
    else if (v === true) el.setAttribute(k, "")
    else el.setAttribute(k, v)
  }
  for (const kid of kids.flat()) if (kid != null && kid !== false) el.append(kid.nodeType ? kid : document.createTextNode(String(kid)))
  return el
}

async function api(path, body) {
  const res = await fetch("/setup/api/" + path, {
    method: body === undefined ? "GET" : "POST",
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.error || `The setup server answered ${res.status}.`)
  return data
}

async function act(fn) {
  busy = true
  error = ""
  render()
  try {
    await fn()
  } catch (e) {
    error = e.message
  } finally {
    busy = false
    render()
  }
}

function go(next) {
  step = next
  clearInterval(timer)
  timer = null
  render()
  app.focus()
}

function stepper() {
  const at = STEPS.findIndex(([k]) => k === step)
  return h("ol", { class: "steps", "aria-label": "Steps" },
    STEPS.map(([k, label], i) => h("li", { class: i < at ? "done" : "", "aria-current": i === at ? "step" : false }, `${i + 1}. ${label}`)))
}

function errorBox() {
  return h("p", { class: "error", role: "alert" }, error)
}

// ---- 1. sign in

function signIn() {
  const flow = st.flow
  const hostBox = h("details", { open: st.host !== "github.com" || !st.canCode },
    h("summary", {}, `Host: ${st.host}`),
    h("label", { for: "host" }, "GitHub host"),
    h("input", { id: "host", type: "text", value: st.host, autocomplete: "off", spellcheck: "false" }),
    h("p", { class: "note" }, "github.com, an Enterprise Server (https://github.example.org) or a GHE.com tenant (https://name.ghe.com)."),
    h("label", { for: "client" }, "Client id of the app for the code sign-in (optional)"),
    h("input", { id: "client", type: "text", value: st.clientId || "", autocomplete: "off", spellcheck: "false" }),
    h("p", { class: "note" }, "The OAuth app or GitHub App registered on this host, with the device flow turned on. Without one, paste a token below."),
    h("div", { class: "row" }, h("button", { type: "button", disabled: busy, on: { click: () => {
      act(async () => { st = await api("host", { host: document.getElementById("host").value, clientId: document.getElementById("client").value }) })
    } } }, "Use this host")))

  const code = st.canCode
    ? h("div", {},
        flow.state === "pending" ? pending(flow) : h("div", { class: "row" },
          h("button", { type: "button", class: "primary", disabled: busy, on: { click: startCode } }, "Sign in with GitHub"),
          h("span", { class: "note" }, "You confirm on github.com; SCP only reads.")),
        flow.state === "failed" ? h("p", { class: "error", role: "alert" }, flow.error) : null)
    : h("p", { class: "note" }, "There is no app client id for this host, so the code sign-in is not offered. Paste a token, or give a client id under Host.")

  const cli = st.canCli
    ? h("div", { class: "row" },
        h("button", { type: "button", disabled: busy, on: { click: () => act(async () => { st = await api("cli", {}); if (st.user) await toAccounts() }) } },
          "Use my GitHub CLI login"),
        h("span", { class: "note" }, "Takes the token `gh` already holds for this host. It stays on this computer and is sealed there; SCP only reads. That token may be allowed to write, so a fine-grained token is the safer grant."))
    : null

  const token = h("details", { open: !st.canCode && !st.canCli },
    h("summary", {}, "Use a token instead"),
    h("p", { class: "note" }, "A fine-grained token with read access to Contents and Metadata (and Pull requests, Issues for those pages), or a classic one with the scopes repo and read:org. It is sent to your own computer only and sealed there."),
    h("form", { on: { submit: (e) => {
      e.preventDefault()
      const input = document.getElementById("token")
      const value = input.value
      input.value = ""
      act(async () => { st = await api("token", { token: value }); if (st.user) await toAccounts() })
    } } },
      h("label", { for: "token" }, "Token"),
      h("input", { id: "token", type: "password", autocomplete: "off", spellcheck: "false" }),
      h("div", { class: "row" }, h("button", { type: "submit", class: "primary", disabled: busy }, "Check the token"))))

  return h("section", { class: "card" }, h("h2", {}, "Sign in to GitHub"), code, cli, token, hostBox)
}

function pending(flow) {
  schedulePoll()
  return h("div", {},
    h("p", {}, "Open GitHub and enter this code:"),
    h("p", {}, h("span", { class: "code", "aria-label": "Code " + flow.userCode.split("").join(" ") }, flow.userCode)),
    h("div", { class: "row" },
      h("a", { href: flow.verificationUri, target: "_blank", rel: "noopener noreferrer" }, h("button", { type: "button", class: "primary" }, "Open GitHub")),
      h("button", { type: "button", on: { click: () => act(async () => { st = await api("signout", {}) }) } }, "Cancel")),
    h("p", { class: "note", role: "status" }, "Waiting for you to confirm on GitHub…"))
}

function schedulePoll() {
  if (timer) return
  timer = setInterval(async () => {
    try {
      const next = await api("state")
      if (next.flow.state !== "pending") {
        clearInterval(timer)
        timer = null
        st = next
        if (st.user) await toAccounts()
        else render()
      }
    } catch (e) {
      error = e.message
      render()
    }
  }, 2000)
}

async function startCode() {
  await act(async () => {
    st = await api("device", {})
    if (st.flow.verificationUri) window.open(st.flow.verificationUri, "_blank", "noopener,noreferrer")
  })
}

async function toAccounts() {
  accounts = new Set([st.user, ...(st.orgs || [])])
  listing = null
  go("accounts")
}

// ---- 2. accounts

function accountsStep() {
  const all = [st.user, ...(st.orgs || [])]
  return h("section", { class: "card" },
    h("h2", {}, `Signed in as ${st.user}`),
    h("p", { class: "note" }, st.ends ? `This sign-in ends ${new Date(st.ends).toLocaleString()}; run scp personal again then.` : "Which accounts should the portal show?"),
    h("ul", { class: "plain" }, all.map((a) => h("li", {},
      h("input", { type: "checkbox", id: "acct-" + a, checked: accounts.has(a), on: { change: (e) => { e.target.checked ? accounts.add(a) : accounts.delete(a); render() } } }),
      h("label", { for: "acct-" + a, class: "inline" }, a, a === st.user ? " (you)" : "")))),
    h("div", { class: "row" },
      h("button", { type: "button", class: "primary", disabled: busy || accounts.size === 0, on: { click: loadRepos } }, "Continue"),
      h("button", { type: "button", class: "link", on: { click: () => act(async () => { st = await api("signout", {}); go("signin") }) } }, "Sign in as someone else")))
}

async function loadRepos() {
  await act(async () => {
    listing = await api("repos", { accounts: [...accounts] })
    products = []
    assign = {}
    for (const r of listing.repos) assign[r.name] = ""
    for (const p of listing.suggested) {
      const key = "p" + nextKey++
      products.push({ key, name: p.name })
      for (const n of p.repos) assign[n] = key
    }
    go("products")
  })
}

// ---- 3. products

function counts() {
  const per = {}
  let hidden = 0
  for (const v of Object.values(assign)) {
    if (v === "hide") hidden++
    else if (v) per[v] = (per[v] || 0) + 1
  }
  return { per, hidden }
}

function problem() {
  const { per, hidden } = counts()
  const lim = listing.limits
  if (hidden > lim.hidden) return `At most ${lim.hidden} repositories can be hidden one by one; leave an account out instead.`
  for (const p of products) {
    if ((per[p.key] || 0) > 0 && !p.name.trim()) return "Give every product that has repositories a name."
    if ((per[p.key] || 0) > lim.perProduct) return `A product holds at most ${lim.perProduct} repositories.`
  }
  const names = products.filter((p) => per[p.key]).map((p) => p.name.trim().toLowerCase())
  if (new Set(names).size !== names.length) return "Two products have the same name."
  if (names.length > lim.products) return `At most ${lim.products} products.`
  return ""
}

function productsStep() {
  const { per, hidden } = counts()
  const issue = problem()
  const rows = listing.repos.map((r) => h("tr", {},
    h("td", {}, h("label", { for: "r-" + r.name, class: "sr" }, "Product of " + r.name), r.owner + "/" + r.name),
    h("td", {}, h("span", { class: "badge" + (r.private ? " private" : "") }, r.private ? "private" : "public"), r.archived ? h("span", { class: "badge" }, "archived") : null),
    h("td", {}, productSelect(r))))
  return h("section", {},
    h("div", { class: "card" },
      h("h2", {}, "Sort your repositories into products"),
      h("p", { class: "note" }, `${listing.repos.length} repositories of the accounts you chose (forks left out, one entry for a name that two accounts share)${listing.more ? ", found among the 500 most recently pushed; older ones exist and are not listed" : ""}. A product is a group on the dashboard. The suggestions come from the first word of the names; change anything. A repository without a product still shows, under Other.`),
      h("div", { class: "products" }, products.map((p) => h("div", { class: "product" },
        h("label", { for: "n-" + p.key, class: "sr" }, "Product name"),
        h("input", { id: "n-" + p.key, type: "text", value: p.name, maxlength: "120", on: { input: (e) => { p.name = e.target.value; refreshOptions() } } }),
        h("span", { class: "count" }, `${per[p.key] || 0} repos`),
        h("button", { type: "button", "aria-label": "Remove " + p.name, on: { click: () => removeProduct(p.key) } }, "Remove")))),
      h("div", { class: "row" }, h("button", { type: "button", on: { click: addProduct } }, "Add a product"))),
    h("div", { class: "card" },
      h("div", { class: "scroll" }, h("table", {}, h("thead", {}, h("tr", {}, h("th", {}, "Repository"), h("th", {}, ""), h("th", {}, "Product"))), h("tbody", {}, rows))),
      hidden ? h("p", { class: "note" }, `${hidden} hidden from the portal.`) : null),
    issue ? h("p", { class: "error", role: "alert" }, issue) : null,
    h("div", { class: "row" },
      h("button", { type: "button", class: "primary", disabled: busy || !!issue, on: { click: finish } }, "Start my portal"),
      h("button", { type: "button", on: { click: () => go("accounts") } }, "Back")))
}

function productSelect(r) {
  const sel = h("select", { id: "r-" + r.name, "data-repo": r.name, on: { change: (e) => { assign[r.name] = e.target.value; render() } } })
  fillOptions(sel, assign[r.name])
  return sel
}

function fillOptions(sel, value) {
  sel.replaceChildren(
    h("option", { value: "" }, "Other (no product)"),
    ...products.map((p) => h("option", { value: p.key, "data-product": p.key }, p.name || "(unnamed)")),
    h("option", { value: "hide" }, "Hide from the portal"))
  sel.value = value
}

// refreshOptions renames the options in place, so typing a product's name keeps the focus.
function refreshOptions() {
  for (const o of document.querySelectorAll("option[data-product]")) {
    const p = products.find((x) => x.key === o.dataset.product)
    if (p) o.textContent = p.name || "(unnamed)"
  }
}

function addProduct() {
  products.push({ key: "p" + nextKey++, name: "" })
  render()
  const inputs = document.querySelectorAll(".product input")
  inputs[inputs.length - 1]?.focus()
}

function removeProduct(key) {
  products = products.filter((p) => p.key !== key)
  for (const n of Object.keys(assign)) if (assign[n] === key) assign[n] = ""
  render()
}

async function finish() {
  const lists = {}
  const hide = []
  for (const [n, v] of Object.entries(assign)) {
    if (v === "hide") hide.push(n)
    else if (v) (lists[v] ||= []).push(n)
  }
  const out = products.filter((p) => lists[p.key]).map((p) => ({ name: p.name.trim(), repos: lists[p.key].sort() }))
  await act(async () => {
    st = await api("finish", { accounts: [...accounts], products: out, hidden: hide.sort() })
    go("start")
    waitForPortal()
  })
}

// ---- 4. start

function startStep() {
  return h("section", { class: "card" }, h("h2", {}, "Starting your portal"),
    h("p", { role: "status" }, "Your settings are saved. The portal is starting; it opens signed in when it is ready (the first sync of your repositories takes a minute or two)."),
    error ? errorBox() : null)
}

async function waitForPortal() {
  for (let i = 0; i < 120; i++) {
    await new Promise((r) => setTimeout(r, 1000))
    try {
      const res = await fetch("/api/v1/me", { headers: { Accept: "application/json" } })
      if (res.ok && (res.headers.get("content-type") || "").includes("json")) {
        location.href = st.launch
        return
      }
    } catch {
      // the setup server has stopped and the portal is not up yet
    }
  }
  error = "The portal did not start within two minutes. Look at the terminal."
  render()
}

// ---- the page

function render() {
  if (!st) return
  const body = { signin: signIn, accounts: accountsStep, products: productsStep, start: startStep }[step]()
  app.replaceChildren(
    ...[
      h("h1", {}, "Set up your portal"),
      h("p", { class: "lede" }, "Sign in to GitHub once, choose what to show, and sort it into products. Everything stays on this computer."),
      stepper(),
      error && step !== "start" ? errorBox() : null,
      body,
    ].filter(Boolean),
  )
}

try {
  st = await api("state")
  if (st.done) step = "start"
  else if (st.user) {
    accounts = new Set([st.user, ...(st.orgs || [])])
    step = "accounts"
  }
  render()
} catch (e) {
  app.replaceChildren(h("p", { class: "error", role: "alert" }, e.message))
}
