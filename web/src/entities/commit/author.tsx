/**
 * Author is a person of the commits in its short form: the GitHub login when GitHub links the commits to an account (shorter, and
 * how the team knows each other), else the name in the commits; the full name is the hover text.
 */
export function Author({ name, login }: { name?: string; login?: string }) {
  const shown = login || name || "unknown"
  return <span title={login && name && name !== login ? name : undefined}>{shown}</span>
}
