// Package apps is the inventory of MCP Apps this server ships.
//
// An MCP App is a tool whose result a host can render as an interactive view
// instead of, or alongside, plain text. The MCP Apps extension defines how:
// the tool names a resource under the ui:// scheme in its _meta.ui, that
// resource is a self-contained HTML document served with the
// text/html;profile=mcp-app MIME type, and the host loads it in a sandboxed
// iframe and talks to it with JSON-RPC over postMessage.
//
// The go-sdk knows nothing of the extension, so this package carries the
// conventions -- the scheme, the MIME type, the _meta shapes -- in one place,
// and the tests hold every registered app to them. A host without the
// extension ignores _meta and receives the ordinary structured result, so an
// app never costs a plain client anything.
//
// Every app is inline: no external script, stylesheet, image, or connection.
// The default CSP a host applies when an app declares none forbids all of
// those, and declaring domains would trade the sandbox for a dependency on a
// CDN being reachable from wherever the host runs. Nothing here needs it.
package apps

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
)

// MIMEType is what the extension requires an app's HTML resource to declare.
// A host uses it, not the ui:// scheme alone, to decide the resource is a
// renderable view rather than a document to hand to the model.
const MIMEType = "text/html;profile=mcp-app"

// Scheme is the URI scheme every app resource must use.
const Scheme = "ui://"

// ProtocolVersion is the MCP Apps protocol version each view negotiates in
// its ui/initialize request.
const ProtocolVersion = "2025-06-18"

// App is one renderable view and the tool whose result it renders.
type App struct {
	// Tool is the name of the tool whose result this app renders. The tool
	// itself is registered by the server package; this is the link.
	Tool string

	// URI is the ui:// resource the host fetches to obtain the view.
	URI string

	// Name and Description are the resource's listing metadata.
	Name        string
	Description string

	// HTML is the complete, self-contained document.
	HTML string

	// PrefersBorder asks the host to draw the view with a visible border and
	// background, which suits a dashboard-shaped app that does not fill the
	// conversation width on its own.
	PrefersBorder bool
}

//go:embed ui/*.html
var htmlFS embed.FS

// InventoryURI is the resource behind the inventory tool.
const InventoryURI = "ui://truenas/inventory"

// All is the inventory of apps this server serves. Adding an app means
// adding an entry here and the tool it names; the tests take care of holding
// it to the extension's conventions.
func All() []App {
	return []App{
		{
			Tool: "inventory",
			URI:  InventoryURI,
			Name: "TrueNAS inventory",
			Description: "Interactive view of the inventory tool's result: pools, datasets, " +
				"apps, virtual machines, shares, and alerts on the target, with a refresh control.",
			HTML:          mustHTML("ui/inventory.html"),
			PrefersBorder: true,
		},
	}
}

// ByTool returns the app that renders a tool's result, if one does.
func ByTool(tool string) (App, bool) {
	for _, a := range All() {
		if a.Tool == tool {
			return a, true
		}
	}
	return App{}, false
}

// ByURI returns the app served at a resource URI, if any.
func ByURI(uri string) (App, bool) {
	for _, a := range All() {
		if a.URI == uri {
			return a, true
		}
	}
	return App{}, false
}

// ToolMeta is the _meta a tool carries to tell a host which resource renders
// its result. visibility is left at its default of both model and app: the
// model may still call the tool directly, and the view may call it again to
// refresh.
func ToolMeta(a App) map[string]any {
	return map[string]any{
		"ui": map[string]any{
			"resourceUri": a.URI,
		},
	}
}

// ResourceMeta is the _meta an app's resource carries. No csp block is
// declared, on purpose: the host's default policy is exactly the one an
// inline-only app wants, and stating it would only give a reader something
// to wonder about.
func ResourceMeta(a App) map[string]any {
	return map[string]any{
		"ui": map[string]any{
			"prefersBorder": a.PrefersBorder,
		},
	}
}

// Validate reports the first way an app departs from the extension's
// conventions, so a registered app is known to be loadable by a conforming
// host before any host tries.
func Validate(a App) error {
	switch {
	case a.Tool == "":
		return fmt.Errorf("app %q names no tool", a.URI)
	case !strings.HasPrefix(a.URI, Scheme):
		return fmt.Errorf("app %q: resource URI must start with %s", a.URI, Scheme)
	case a.Name == "":
		return fmt.Errorf("app %q has no name", a.URI)
	case strings.TrimSpace(a.HTML) == "":
		return fmt.Errorf("app %q has no HTML", a.URI)
	case !strings.Contains(a.HTML, "ui/initialize"):
		return fmt.Errorf("app %q never sends ui/initialize, so no host would deliver it a result", a.URI)
	}
	if ref, ok := externalReference(a.HTML); ok {
		return fmt.Errorf("app %q references %s, which the default CSP blocks", a.URI, ref)
	}
	return nil
}

// externalReference finds a src or href pointing off-document. Anything
// fetched from a network origin is refused by the host's default CSP, so an
// app that carried one would render broken rather than merely slowly.
func externalReference(html string) (string, bool) {
	for _, attr := range []string{`src="`, `href="`, `src='`, `href='`} {
		rest := html
		for {
			i := strings.Index(rest, attr)
			if i < 0 {
				break
			}
			rest = rest[i+len(attr):]
			end := strings.IndexAny(rest, `"'`)
			if end < 0 {
				break
			}
			value := rest[:end]
			lower := strings.ToLower(value)
			if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "//") {
				return value, true
			}
		}
	}
	return "", false
}

func mustHTML(name string) string {
	b, err := fs.ReadFile(htmlFS, name)
	if err != nil {
		panic(fmt.Sprintf("apps: embedded %s missing: %v", name, err))
	}
	return string(b)
}
