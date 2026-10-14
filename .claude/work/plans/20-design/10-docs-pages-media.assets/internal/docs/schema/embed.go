// Package schema embeds the JSON Schemas of the repository descriptor and of document front matter. It has no
// dependencies (rank 0); the loader that uses the schema lives in package docs.
package schema

import _ "embed"

//go:embed scp-v1.json
var scpV1 []byte

//go:embed frontmatter-v1.json
var frontMatterV1 []byte

// ScpV1 returns the descriptor schema served at /schemas/scp/v1.json. The slice must not be modified.
func ScpV1() []byte { return scpV1 }

// FrontMatterV1 returns the front-matter schema served at /schemas/scp/frontmatter-v1.json.
func FrontMatterV1() []byte { return frontMatterV1 }

// Files lists the served schema files by name (the {file} of GET /schemas/scp/{file}).
func Files() map[string][]byte {
	return map[string][]byte{"v1.json": scpV1, "frontmatter-v1.json": frontMatterV1}
}
