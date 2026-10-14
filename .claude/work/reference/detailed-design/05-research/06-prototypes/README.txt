Prototype snippets used to verify claims in ../06-web-ui-badges-security.md (2026-10-13).
Throwaway, not production code. Go 1.27.1, bluemonday v1.0.27 + golang.org/x/net v0.61.0,
goldmark/v2 v2.1.6, x/image v0.47.0. webapp_test.go needs a Vite dist/ (see report section 3) and
badge_test.go needs DejaVuSans.ttf (npm dejavu-fonts-ttf@2.37.3) - both skip/fail without them.
