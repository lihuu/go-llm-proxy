package web

import "embed"

// FS embeds the web UI static assets (HTML templates, CSS, JS) so the proxy
// stays a single self-contained binary with no external file dependencies.
//
// Subdirectories under web/ are embedded by name:
//   - configpage/   config generator UI served at GET /
//   - usagepage/    usage dashboard + login pages served at /usage
//
// Usage from other packages:
//
//	tmpl, err := template.ParseFS(web.FS, "configpage/index.html")
//	data, err := web.FS.ReadFile("usagepage/dashboard.html")
//
//go:embed configpage usagepage
var FS embed.FS