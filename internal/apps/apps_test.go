package apps

import (
	"strings"
	"testing"
)

// Every app in the inventory must be loadable by a conforming host. These
// tests are the contract a new entry in All() is held to.

func TestEveryAppValidates(t *testing.T) {
	if len(All()) == 0 {
		t.Fatal("the app inventory is empty")
	}
	for _, a := range All() {
		if err := Validate(a); err != nil {
			t.Errorf("app %q: %v", a.URI, err)
		}
	}
}

func TestAppURIsAndToolsAreUnique(t *testing.T) {
	uris := map[string]bool{}
	tools := map[string]bool{}
	for _, a := range All() {
		if uris[a.URI] {
			t.Errorf("resource URI %q is registered twice", a.URI)
		}
		if tools[a.Tool] {
			t.Errorf("tool %q has two apps; a tool names one resource", a.Tool)
		}
		uris[a.URI] = true
		tools[a.Tool] = true
	}
}

func TestLookupsAgree(t *testing.T) {
	for _, a := range All() {
		byTool, ok := ByTool(a.Tool)
		if !ok || byTool.URI != a.URI {
			t.Errorf("ByTool(%q) = %v, %v", a.Tool, byTool.URI, ok)
		}
		byURI, ok := ByURI(a.URI)
		if !ok || byURI.Tool != a.Tool {
			t.Errorf("ByURI(%q) = %v, %v", a.URI, byURI.Tool, ok)
		}
	}
	if _, ok := ByTool("no_such_tool"); ok {
		t.Error("ByTool found an app for a tool that has none")
	}
	if _, ok := ByURI("ui://nowhere"); ok {
		t.Error("ByURI found an app at a URI that has none")
	}
}

// The _meta shapes are the extension's, not ours: a host looks for exactly
// these keys.
func TestToolMetaNamesTheResource(t *testing.T) {
	app, _ := ByTool("inventory")
	meta := ToolMeta(app)
	ui, ok := meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("tool _meta lacks a ui object: %#v", meta)
	}
	if ui["resourceUri"] != InventoryURI {
		t.Errorf("_meta.ui.resourceUri = %v, want %s", ui["resourceUri"], InventoryURI)
	}
}

func TestResourceMetaDeclaresNoCSP(t *testing.T) {
	app, _ := ByTool("inventory")
	ui, ok := ResourceMeta(app)["ui"].(map[string]any)
	if !ok {
		t.Fatal("resource _meta lacks a ui object")
	}
	if _, declared := ui["csp"]; declared {
		t.Error("an inline-only app must not declare a csp; the host default is the intended policy")
	}
	if ui["prefersBorder"] != true {
		t.Error("the inventory is a bordered dashboard")
	}
}

func TestInventoryHTMLIsSelfContained(t *testing.T) {
	app, _ := ByTool("inventory")
	html := app.HTML

	for _, forbidden := range []string{`<script src`, `<link rel="stylesheet"`, `@import`, `fetch(`, `XMLHttpRequest`} {
		if strings.Contains(html, forbidden) {
			t.Errorf("inventory HTML contains %q, which the host's default CSP blocks", forbidden)
		}
	}
	// It must speak the lifecycle a host expects, and render the fields the
	// inventory tool produces.
	for _, want := range []string{
		"ui/initialize", "ui/notifications/initialized", "ui/notifications/tool-result",
		"ui/notifications/tool-input", "ui/notifications/size-changed", "ui/resource-teardown",
		ProtocolVersion, "structuredContent", `name: 'inventory'`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("inventory HTML should contain %q", want)
		}
	}
}

func TestValidateRejectsDepartures(t *testing.T) {
	good, _ := ByTool("inventory")

	cases := map[string]func(App) App{
		"no tool":        func(a App) App { a.Tool = ""; return a },
		"wrong scheme":   func(a App) App { a.URI = "truenas://inventory"; return a },
		"no name":        func(a App) App { a.Name = ""; return a },
		"empty html":     func(a App) App { a.HTML = " "; return a },
		"no initialize":  func(a App) App { a.HTML = "<html></html>"; return a },
		"external src":   func(a App) App { a.HTML = `<script src="https://cdn.example/sdk.js"></script>ui/initialize`; return a },
		"external href":  func(a App) App { a.HTML = `<link href='//cdn.example/a.css'>ui/initialize`; return a },
		"external image": func(a App) App { a.HTML = `<img src="http://x/y.png">ui/initialize`; return a },
	}
	for name, mutate := range cases {
		if err := Validate(mutate(good)); err == nil {
			t.Errorf("%s: Validate accepted it", name)
		}
	}

	// Relative and fragment references are fine: the document is one file.
	inline := good
	inline.HTML = `<a href="#pools">pools</a><img src="data:image/png;base64,AA==">ui/initialize`
	if err := Validate(inline); err != nil {
		t.Errorf("inline references should validate: %v", err)
	}
}
