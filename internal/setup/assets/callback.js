// GitHub sent the person back here with the App's code, which the server has already used. The token the setup page works with comes
// along (only for the state the page made), is kept for this tab, and the page goes on from here.
const given = document.querySelector('meta[name="setup-token"]')?.content
if (given) {
  try {
    sessionStorage.setItem("scp-setup-token", given)
  } catch {
    // no storage: the page cannot go on after a reload
  }
}
location.replace("/setup/")
