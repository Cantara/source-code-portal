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

let readStarted = 0 // when this page saw the read begin, for the time left

function accountsStep() {
  const all = [st.user, ...(st.orgs || [])]
  const job = st.job
  if (job && job.state === "running") return readingCard(job)
  return h("section", { class: "card" },
    h("h2", {}, `Signed in as ${st.user}`),
    h("p", { class: "note" }, st.ends ? `This sign-in ends ${new Date(st.ends).toLocaleString()}; run scp personal again then.` : "Which accounts should the portal show?"),
    h("ul", { class: "plain" }, all.map((a) => h("li", {},
      h("input", { type: "checkbox", id: "acct-" + a, checked: accounts.has(a), on: { change: (e) => { e.target.checked ? accounts.add(a) : accounts.delete(a); render() } } }),
      h("label", { for: "acct-" + a, class: "inline" }, a, a === st.user ? " (you)" : "")))),
    job && job.state === "failed" ? h("p", { class: "error", role: "alert" }, job.error) : null,
    h("div", { class: "row" },
      h("button", { type: "button", class: "primary", disabled: busy || accounts.size === 0, on: { click: loadRepos } }, job && job.state === "failed" ? "Try again" : "Continue"),
      h("button", { type: "button", class: "link", on: { click: () => act(async () => { st = await api("signout", {}); go("signin") }) } }, "Sign in as someone else")))
}

// readingCard shows the repository read: what is being read, how far it is and, once there is a rate, how long it will take.
function readingCard(job) {
  const known = job.total > 0
  const left = known && job.done > 0 && readStarted ? Math.max(0, Math.round(((Date.now() - readStarted) / job.done) * (job.total - job.done) / 1000)) : null
  return h("section", { class: "card", "aria-busy": "true" },
    h("h2", {}, "Reading your repositories"),
    h("p", {}, job.counting ? `Counting the repositories of your ${job.steps} account${job.steps === 1 ? "" : "s"}` : `${job.account} (account ${job.step} of ${job.steps})`),
    h("progress", { class: "bar", max: known ? job.total : false, value: known ? job.done : false, "aria-label": "Repositories read" }),
    h("p", { class: "note", role: "status" },
      job.counting ? "This takes a few seconds." : known ? `${job.done} of about ${job.total} repositories` : `${job.done} repositories so far`,
      left != null && left > 2 ? `, about ${left < 60 ? left + " seconds" : Math.ceil(left / 60) + " minutes"} left` : ""),
    h("p", { class: "note" }, "GitHub hands them over a hundred at a time. Keep this page open; nothing is changed on GitHub."),
    h("div", { class: "row" }, h("button", { type: "button", on: { click: cancelRead } }, "Cancel")))
}

async function loadRepos() {
  error = ""
  readStarted = Date.now()
  try {
    st = await api("list", { accounts: [...accounts] })
  } catch (e) {
    error = e.message
  }
  render()
  watchRead()
}

// watchRead polls the read until it ends, then opens the products or shows what went wrong.
function watchRead() {
  clearInterval(timer)
  if (!st.job || st.job.state !== "running") {
    if (st.job && st.job.state === "done" && step === "accounts") void openProducts()
    return
  }
  timer = setInterval(async () => {
    try {
      st = await api("state")
    } catch (e) {
      clearInterval(timer)
      timer = null
      error = e.message
      render()
      return
    }
    if (st.job && st.job.state === "running") return render()
    clearInterval(timer)
    timer = null
    if (st.job && st.job.state === "done") await openProducts()
    else render()
  }, 500)
}

async function cancelRead() {
  clearInterval(timer)
  timer = null
  try {
    st = await api("list/cancel", {})
  } catch (e) {
    error = e.message
  }
  render()
}

async function openProducts() {
  await act(async () => {
    listing = await api("repos", { accounts: [...accounts] })
    applySuggestion()
    selected = new Set()
    query = ""
    view = "all"
    rowsShown = {}
    drafts = {}
    go("products")
    schedulePreview()
  })
}

// ---- 3. products: the repositories at the left, the products at the right. A repository is dragged (or moved with the menu) into a
// product; the server works out the rules the product's repositories share (a name that starts the same, a topic), so repositories
// created later fit by themselves. Nothing is written until Start.

let derived = {} // product key -> what the server worked out for it: rules, pinned repositories
let derivedId = 0 // a late answer to an older question is dropped
let previewTimer = null
let previewError = ""
let selected = new Set()
let drag = [] // the names being dragged
let query = ""
let view = "all" // all | unassigned | hidden
let rowsShown = {} // account -> rows drawn (the lists can be long)
let lastPick = null // the repository last ticked, for shift-click ranges
let drafts = {} // product key -> { kind, value, result } the rule being written
let draftTimer = null

const PAGE = 150
const KINDS = [
  ["prefix", "Name starts with"],
  ["topic", "Has the topic"],
  ["glob", "Name like (* and ?)"],
  ["regexp", "Name matches (regular expression)"],
]

const membersOf = (key) => listing.repos.filter((r) => assign[r.name] === key).map((r) => r.name)
const hiddenNames = () => listing.repos.filter((r) => assign[r.name] === "hide").map((r) => r.name)
const withMembers = () => products.filter((p) => membersOf(p.key).length)
const wire = (p) => ({ name: p.name.trim(), repos: membersOf(p.key).sort(), rules: p.rules, fixed: p.fixed || undefined })

function say(rule) {
  const v = rule.value
  return { prefix: `name starts with “${v}”`, topic: `topic “${v}”`, glob: `name like “${v}”`, regexp: `name matches /${v}/`, exact: v }[rule.kind] || v
}

function addProduct(name) {
  const key = "p" + nextKey++
  products.push({ key, name, rules: [], fixed: false })
  return key
}

function suggestName(names) {
  const words = names.map((n) => n.split(/[-_.]/)[0].toLowerCase())
  let base = "New product"
  if (names.length > 1 && words[0].length >= 2 && words.every((w) => w === words[0])) base = words[0][0].toUpperCase() + words[0].slice(1)
  let name = base
  for (let i = 2; products.some((p) => p.name.trim().toLowerCase() === name.toLowerCase()); i++) name = `${base} ${i}`
  return name
}

function moveTo(names, target) {
  if (target === "new") target = addProduct(suggestName(names))
  for (const n of names) assign[n] = target === "none" ? "" : target
  selected = new Set([...selected].filter((n) => !names.includes(n)))
  render()
  schedulePreview()
}

function schedulePreview() {
  clearTimeout(previewTimer)
  previewTimer = setTimeout(runPreview, 250)
}

async function runPreview() {
  const mine = withMembers()
  const id = ++derivedId
  try {
    const out = await api("rules", { accounts: [...accounts], hidden: hiddenNames(), products: mine.map(wire) })
    if (id !== derivedId) return
    derived = {}
    out.products.forEach((d, i) => { derived[mine[i].key] = d })
    previewError = ""
  } catch (e) {
    if (id !== derivedId) return
    previewError = e.message
  }
  if (step === "products") render()
}

function problem() {
  const lim = listing.limits
  if (previewError) return previewError
  const hidden = hiddenNames().length
  if (hidden > lim.hidden) return `At most ${lim.hidden} repositories can be hidden one by one; leave an account out instead.`
  const mine = withMembers()
  for (const p of mine) {
    if (!p.name.trim()) return "Give every product a name."
    if (derived[p.key] && derived[p.key].tooMany) return `“${p.name}” needs more than ${lim.matchers} entries. Add a rule that covers many of its repositories, or split it.`
  }
  const names = mine.map((p) => p.name.trim().toLowerCase())
  if (new Set(names).size !== names.length) return "Two products have the same name."
  if (names.length > lim.products) return `At most ${lim.products} products.`
  return ""
}

function dropTarget(el, target) {
  el.addEventListener("dragover", (e) => { if (drag.length) { e.preventDefault(); el.classList.add("over") } })
  el.addEventListener("dragleave", () => el.classList.remove("over"))
  el.addEventListener("drop", (e) => {
    e.preventDefault()
    el.classList.remove("over")
    if (drag.length) moveTo(drag, target)
    drag = []
  })
  return el
}

function draggable(el, name) {
  el.setAttribute("draggable", "true")
  el.addEventListener("dragstart", (e) => {
    drag = selected.has(name) ? [...selected] : [name]
    e.dataTransfer.effectAllowed = "move"
    e.dataTransfer.setData("text/plain", drag.join(", "))
    el.classList.add("dragging")
  })
  el.addEventListener("dragend", () => { drag = []; el.classList.remove("dragging") })
  return el
}

// -- the repositories

function visibleRepos() {
  const q = query.trim().toLowerCase()
  return listing.repos.filter((r) => {
    const a = assign[r.name] || ""
    if (view === "unassigned" && a) return false
    if (view === "hidden" && a !== "hide") return false
    return !q || r.name.toLowerCase().includes(q) || (r.topics || []).some((t) => t.includes(q))
  })
}

function pick(name, on, shift, visible) {
  if (shift && lastPick && lastPick !== name) {
    const a = visible.indexOf(lastPick)
    const b = visible.indexOf(name)
    if (a >= 0 && b >= 0) for (const n of visible.slice(Math.min(a, b), Math.max(a, b) + 1)) on ? selected.add(n) : selected.delete(n)
  } else on ? selected.add(name) : selected.delete(name)
  lastPick = name
  render()
}

function repoRow(r, visible) {
  const a = assign[r.name] || ""
  const owner = a === "hide" ? "Hidden" : a ? (products.find((p) => p.key === a) || {}).name : ""
  return draggable(h("li", { class: "repo" + (selected.has(r.name) ? " sel" : "") + (a ? " in" : "") },
    h("input", { type: "checkbox", id: "pick-" + r.name, checked: selected.has(r.name), "aria-label": "Select " + r.name,
      on: { click: (e) => pick(r.name, e.target.checked, e.shiftKey, visible) } }),
    h("span", { class: "name" }, r.name),
    r.private ? h("span", { class: "badge private" }, "private") : null,
    r.archived ? h("span", { class: "badge" }, "archived") : null,
    (r.topics || []).slice(0, 3).map((t) => h("span", { class: "chip" }, t)),
    owner ? h("span", { class: "in-product" }, owner) : null), r.name)
}

function reposPanel() {
  const visible = visibleRepos()
  const names = visible.map((r) => r.name)
  const sections = [...accounts].map((acct) => {
    const all = listing.repos.filter((r) => r.owner.toLowerCase() === acct.toLowerCase())
    const rows = visible.filter((r) => r.owner.toLowerCase() === acct.toLowerCase())
    const free = all.filter((r) => !assign[r.name]).length
    const n = rowsShown[acct] || PAGE
    return h("details", { open: true, class: "account", "data-id": "acct-" + acct },
      h("summary", {}, `${acct} · ${all.length} repositories${free ? `, ${free} without a product` : ""}`),
      h("ul", { class: "plain repos" }, rows.slice(0, n).map((r) => repoRow(r, names))),
      rows.length > n ? h("button", { type: "button", class: "link", on: { click: () => { rowsShown[acct] = n + PAGE; render() } } }, `Show ${Math.min(PAGE, rows.length - n)} more of ${rows.length - n}`) : null,
      rows.length === 0 ? h("p", { class: "note" }, "Nothing here matches.") : null)
  })
  return h("div", { class: "panel" },
    h("h3", {}, "Your repositories"),
    h("div", { class: "toolbar" },
      h("label", { for: "q", class: "sr" }, "Search repositories"),
      h("input", { id: "q", type: "search", placeholder: "Search names and topics", value: query, autocomplete: "off", on: { input: (e) => { query = e.target.value; render() } } }),
      h("label", { for: "view", class: "sr" }, "Show"),
      h("select", { id: "view", on: { change: (e) => { view = e.target.value; render() } } },
        [["all", "All"], ["unassigned", "Without a product"], ["hidden", "Hidden"]].map(([v, l]) => h("option", { value: v, selected: view === v }, l)))),
    h("div", { class: "row tight" },
      h("button", { type: "button", on: { click: () => { for (const n of names) selected.add(n); render() } } }, `Select the ${names.length} shown`),
      selected.size ? h("button", { type: "button", class: "link", on: { click: () => { selected = new Set(); render() } } }, `Clear (${selected.size})`) : null),
    moveBar(),
    h("div", { class: "keep list", id: "repo-list" }, sections))
}

function moveBar() {
  if (!selected.size) return h("p", { class: "note" }, "Drag repositories to a product, or tick them and move them with the menu. Shift-click ticks a range.")
  return h("div", { class: "movebar" },
    h("span", {}, `${selected.size} selected:`),
    h("label", { for: "move", class: "sr" }, "Move the selected repositories to"),
    h("select", { id: "move" },
      h("option", { value: "new" }, "a new product"),
      products.filter((p) => p.name.trim()).map((p) => h("option", { value: p.key }, p.name)),
      h("option", { value: "none" }, "no product (Other)"),
      h("option", { value: "hide" }, "hidden")),
    h("button", { type: "button", class: "primary", on: { click: () => moveTo([...selected], document.getElementById("move").value) } }, "Move"))
}

// -- the products

function zone(title, note, target, count) {
  return dropTarget(h("div", { class: "zone", tabindex: "0", role: "group", "aria-label": title }, h("strong", {}, title), count != null ? h("span", { class: "count" }, count) : null, h("span", { class: "note" }, note)), target)
}

function ruleLine(p, rule) {
  const extra = rule.extraN
    ? h("p", { class: "warn" }, `Also catches ${rule.extraN} repositories no product holds: ${rule.extra.join(", ")}${rule.extraN > rule.extra.length ? ", …" : ""} `,
        h("button", { type: "button", class: "link", on: { click: () => claim(p, rule) } }, "Add them to this product"))
    : null
  const spill = rule.spill && rule.spill.length
    ? h("p", { class: "error" }, `Also catches ${rule.spill.join(", ")} of another product, which will then be in both.`)
    : null
  return h("li", { class: "rule" },
    h("span", { class: "rule-text" }, say(rule)),
    h("span", { class: "meta" }, `covers ${rule.own}`, rule.open ? " · new repositories fit" : ""),
    rule.manual ? h("span", { class: "chip" }, "yours") : null,
    rule.manual ? h("button", { type: "button", class: "link", "aria-label": "Remove the rule " + say(rule), on: { click: () => { p.rules = p.rules.filter((x) => !(x.kind === rule.kind && x.value === rule.value)); render(); schedulePreview() } } }, "Remove") : null,
    extra, spill)
}

async function claim(p, rule) {
  await act(async () => {
    const out = await api("rule-test", { accounts: [...accounts], kind: rule.kind, value: rule.value })
    for (const n of out.names || []) if (!assign[n]) assign[n] = p.key
  })
  schedulePreview()
}

function ruleForm(p) {
  const d = drafts[p.key] || (drafts[p.key] = { kind: "prefix", value: "", result: null })
  const r = d.result
  const ok = r && !r.error && r.matches > 0
  const idv = "rv-" + p.key
  return h("details", { class: "add-rule", "data-id": "ar-" + p.key },
    h("summary", {}, "Add a rule of your own"),
    h("div", { class: "toolbar" },
      h("label", { for: "rk-" + p.key, class: "sr" }, "Kind of rule"),
      h("select", { id: "rk-" + p.key, on: { change: (e) => { d.kind = e.target.value; testDraft(p, d) } } }, KINDS.map(([v, l]) => h("option", { value: v, selected: d.kind === v }, l))),
      h("label", { for: idv, class: "sr" }, "Value"),
      h("input", { id: idv, type: "text", value: d.value, autocomplete: "off", spellcheck: "false", on: { input: (e) => { d.value = e.target.value; testDraft(p, d) } } })),
    r ? h("p", { class: r.error ? "error" : "note", role: "status" }, r.error || `Selects ${r.matches} repositories${r.names.length ? ": " + r.names.slice(0, 6).join(", ") + (r.matches > 6 ? ", …" : "") : ""}`) : null,
    h("button", { type: "button", disabled: !ok, on: { click: () => addRule(p, d) } }, ok ? `Add the rule and take the ${r.names.filter((n) => !assign[n]).length} without a product` : "Add the rule"))
}

function testDraft(p, d) {
  clearTimeout(draftTimer)
  d.result = null
  draftTimer = setTimeout(async () => {
    if (!d.value.trim()) return render()
    try {
      d.result = await api("rule-test", { accounts: [...accounts], kind: d.kind, value: d.value.trim() })
    } catch (e) {
      d.result = { error: e.message }
    }
    render()
  }, 300)
}

function addRule(p, d) {
  p.rules.push({ kind: d.kind, value: d.value.trim() })
  for (const n of d.result.names) if (!assign[n]) assign[n] = p.key
  drafts[p.key] = { kind: d.kind, value: "", result: null }
  render()
  schedulePreview()
}

function productCard(p) {
  const names = membersOf(p.key)
  const d = derived[p.key]
  const open = d ? d.rules.filter((r) => r.open) : []
  const card = h("div", { class: "pcard" },
    h("div", { class: "product" },
      h("label", { for: "n-" + p.key, class: "sr" }, "Product name"),
      h("input", { id: "n-" + p.key, type: "text", value: p.name, maxlength: "120", on: { input: (e) => { p.name = e.target.value; schedulePreview() } } }),
      h("span", { class: "count" }, `${names.length} repos`),
      h("button", { type: "button", on: { click: () => { for (const n of names) assign[n] = ""; products = products.filter((x) => x !== p); render(); schedulePreview() } } }, "Remove")),
    !d ? h("p", { class: "note" }, "Working out the rules…")
      : h("div", {},
          open.length ? h("p", { class: "fits" }, open.length > 3 ? `New repositories that fit any of its ${open.length} open rules join this product by themselves.` : `New repositories whose ${open.map(say).join(" or ")} join this product by themselves.`)
                      : h("p", { class: "note" }, "A fixed list: a new repository goes to Other until you add it."),
          h("ul", { class: "plain rules" }, d.rules.map((r) => ruleLine(p, r))),
          d.pinned.length ? h("details", { "data-id": "pin-" + p.key }, h("summary", {}, `${d.pinned.length} pinned one by one`), h("p", { class: "note" }, d.pinned.join(", "))) : null),
    h("div", { class: "row tight" },
      h("input", { type: "checkbox", id: "fx-" + p.key, checked: !p.fixed, on: { change: (e) => { p.fixed = !e.target.checked; render(); schedulePreview() } } }),
      h("label", { for: "fx-" + p.key, class: "inline" }, "Work out rules so new repositories fit")),
    ruleForm(p),
    h("details", { "data-id": "mem-" + p.key }, h("summary", {}, `Repositories (${names.length})`),
      h("ul", { class: "plain members" }, names.map((n) => draggable(h("li", { class: "repo" }, h("span", { class: "name" }, n), h("button", { type: "button", class: "link", on: { click: () => moveTo([n], "none") } }, "Remove")), n)))))
  return dropTarget(card, p.key)
}

function productsPanel() {
  const { hidden } = counts()
  const unassigned = listing.repos.filter((r) => !assign[r.name]).length
  return h("div", { class: "panel" },
    h("h3", {}, "Your products"),
    h("div", { class: "zones" },
      zone("New product", "Drop repositories here", "new"),
      zone("Other", "No product", "none", unassigned),
      zone("Hidden", "Left out of the portal", "hide", hidden)),
    h("div", { class: "keep list", id: "product-list" }, products.map(productCard)),
    h("div", { class: "row tight" },
      h("button", { type: "button", on: { click: suggestAgain } }, "Suggest products again"),
      h("button", { type: "button", class: "link", on: { click: clearAll } }, "Clear everything")))
}

function counts() {
  let hidden = 0
  for (const v of Object.values(assign)) if (v === "hide") hidden++
  return { hidden }
}

function applySuggestion() {
  products = []
  assign = {}
  derived = {}
  for (const r of listing.repos) assign[r.name] = ""
  for (const p of listing.suggested) {
    const key = addProduct(p.name)
    for (const n of p.repos) assign[n] = key
  }
}

function suggestAgain() { applySuggestion(); selected = new Set(); render(); schedulePreview() }
function clearAll() { products = []; assign = {}; derived = {}; for (const r of listing.repos) assign[r.name] = ""; selected = new Set(); render() }

function productsStep() {
  const issue = problem()
  const n = withMembers().length
  return h("section", {},
    h("div", { class: "card" },
      h("h2", {}, "Sort your repositories into products"),
      h("p", { class: "note" }, `${listing.repos.length} repositories of the accounts you chose (forks left out, one entry for a name that two accounts share)${listing.more ? "; an account with more than 2000 is read up to its 2000 most recently pushed" : ""}. A product is a group on the dashboard. Drag repositories into products; the portal works out what they share, so repositories you create later fit by themselves. Anything left over shows under Other.`)),
    h("div", { class: "editor" }, reposPanel(), productsPanel()),
    h("div", { class: "card" },
      issue ? h("p", { class: "error", role: "alert" }, issue) : h("p", { class: "note" }, n ? `${n} product${n === 1 ? "" : "s"} will be written to personal.yaml; you can edit them there later.` : "No products: everything shows in one group."),
      h("div", { class: "row" },
        h("button", { type: "button", class: "primary", disabled: busy || !!issue, on: { click: finish } }, "Start my portal"),
        h("button", { type: "button", on: { click: () => go("accounts") } }, "Back"))))
}

async function finish() {
  const out = withMembers().map(wire)
  await act(async () => {
    st = await api("finish", { accounts: [...accounts], products: out, hidden: hiddenNames().sort() })
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
  // The page is rebuilt on every change; keep what the person is in the middle of: the focused field and its caret, and the scroll of
  // the two lists.
  const active = document.activeElement
  const focus = active && active.id ? { id: active.id, from: active.selectionStart, to: active.selectionEnd } : null
  const scrolls = {}
  for (const el of app.querySelectorAll(".keep[id]")) scrolls[el.id] = el.scrollTop
  const before = new Set([...app.querySelectorAll("details[data-id]")].map((d) => d.dataset.id))
  const open = new Set([...app.querySelectorAll("details[data-id][open]")].map((d) => d.dataset.id))
  const body = { signin: signIn, accounts: accountsStep, products: productsStep, start: startStep }[step]()
  app.classList.toggle("wide", step === "products")
  app.replaceChildren(
    ...[
      h("h1", {}, "Set up your portal"),
      h("p", { class: "lede" }, "Sign in to GitHub once, choose what to show, and sort it into products. Everything stays on this computer."),
      stepper(),
      error && step !== "start" ? errorBox() : null,
      body,
    ].filter(Boolean),
  )
  for (const el of app.querySelectorAll(".keep[id]")) if (scrolls[el.id]) el.scrollTop = scrolls[el.id]
  for (const d of app.querySelectorAll("details[data-id]")) if (before.has(d.dataset.id)) d.open = open.has(d.dataset.id)
  if (focus) {
    const el = document.getElementById(focus.id)
    if (el) {
      el.focus({ preventScroll: true })
      if (focus.from != null && el.setSelectionRange) {
        try {
          el.setSelectionRange(focus.from, focus.to)
        } catch {
          // a field kind without a caret
        }
      }
    }
  }
}

try {
  st = await api("state")
  if (st.done) step = "start"
  else if (st.user) {
    accounts = new Set([st.user, ...(st.orgs || [])])
    step = "accounts"
    if (st.job && st.job.state === "running") readStarted = Date.now()
  }
  render()
  if (step === "accounts" && st.job && st.job.state === "running") watchRead()
} catch (e) {
  app.replaceChildren(h("p", { class: "error", role: "alert" }, e.message))
}
