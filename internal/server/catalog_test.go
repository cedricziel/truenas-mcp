package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// catalogFixture is one app.available-shaped entry, deliberately carrying the
// bulk a real target's response does -- app_readme and description -- so a
// test can prove that bulk is gone from the default browse and present in
// show.
func catalogFixture(name, category string, installed bool) map[string]any {
	return map[string]any{
		"name":               name,
		"title":              strings.ToUpper(name[:1]) + name[1:],
		"categories":         []string{category},
		"train":              "stable",
		"latest_version":     "1.0.0",
		"latest_app_version": "1.0.0_1",
		"installed":          installed,
		"app_readme":         "<html>a readme nobody asked for by default</html>",
		"description":        "what " + name + " does",
	}
}

// TestCatalogListIsBoundedAndProjected is the end-to-end counterpart to
// TestCatalogListProjectsIdentityAndVersionFields: it proves the projection
// actually reaches a caller through the registered tool, not just that the
// concern declares one. app.available on a real target returns roughly 400
// entries, each carrying an HTML readme -- this fixture is smaller, but limit
// forces the same truncation accounting a real browse would need.
func TestCatalogListIsBoundedAndProjected(t *testing.T) {
	target := newFakeTarget(t)
	target.respond("app.available", []map[string]any{
		catalogFixture("plex", "media", false),
		catalogFixture("jellyfin", "media", true),
		catalogFixture("nextcloud", "productivity", false),
	})
	client := discoveryClient(t, discoverySession(t, target), false)

	res, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "catalog",
		Arguments: map[string]any{"op": "list", "limit": 2},
	})
	if err != nil {
		t.Fatalf("call catalog: %v", err)
	}
	if res.IsError {
		t.Fatalf("catalog returned an error: %v", res.Content)
	}

	out, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content, got %T", res.StructuredContent)
	}

	items, ok := out["result"].([]any)
	if !ok {
		t.Fatalf("expected result to be a list, got %T", out["result"])
	}
	if len(items) != 2 {
		t.Fatalf("result has %d items, want 2 -- limit=2 must bound the browse", len(items))
	}
	if total, _ := out["total"].(float64); total != 3 {
		t.Errorf("total = %v, want 3", out["total"])
	}
	if truncated, _ := out["truncated"].(bool); !truncated {
		t.Error("truncated must be true: 3 entries were cut down to 2")
	}
	if projected, _ := out["projected"].(bool); !projected {
		t.Error("projected must be true: app_readme and description were dropped")
	}

	first, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("expected item to be an object, got %T", items[0])
	}
	if _, ok := first["app_readme"]; ok {
		t.Error("a default browse must not carry app_readme -- that is what blows a caller's context")
	}
	if _, ok := first["installed"]; !ok {
		t.Error("a default browse must carry installed, so a caller need not cross-reference apps(op=\"list\")")
	}
}

// TestCatalogShowReturnsCompleteRecord is the end-to-end counterpart to
// TestCatalogShowFiltersOnNameUsingEquality: show declares no projection, so
// the fields a browse withholds -- app_readme, description -- must still be
// present when a caller asks for one entry by name.
func TestCatalogShowReturnsCompleteRecord(t *testing.T) {
	target := newFakeTarget(t)
	target.respond("app.available", []map[string]any{
		catalogFixture("plex", "media", false),
	})
	client := discoveryClient(t, discoverySession(t, target), false)

	res, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "catalog",
		Arguments: map[string]any{"op": "show", "name": "plex"},
	})
	if err != nil {
		t.Fatalf("call catalog: %v", err)
	}
	if res.IsError {
		t.Fatalf("catalog returned an error: %v", res.Content)
	}

	out, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content, got %T", res.StructuredContent)
	}
	if projected, _ := out["projected"].(bool); projected {
		t.Error("show declares no projection and must never report one")
	}

	items, ok := out["result"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected exactly one entry, got %#v", out["result"])
	}
	entry, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("expected the entry to be an object, got %T", items[0])
	}
	if _, ok := entry["app_readme"]; !ok {
		t.Error("show must return the complete record, including app_readme -- " +
			"no further call should be required to read it")
	}
	if entry["name"] != "plex" {
		t.Errorf("entry name = %v, want plex", entry["name"])
	}

	raw, ok := target.lastParams("app.available")
	if !ok {
		t.Fatal("app.available was never called")
	}
	if !strings.Contains(string(raw), `"plex"`) {
		t.Errorf("expected the name filter to reach the target, got params %s", raw)
	}
}

// TestCatalogListNarrowedByCategorySendsMembershipFilter is the end-to-end
// counterpart to TestCatalogListFiltersOnCategoryUsingMembership: it proves
// the membership operator actually reaches the target, and that the reported
// total is the filtered count -- not the full catalog's -- which is the
// point of filtering at the target rather than post-filtering here.
func TestCatalogListNarrowedByCategorySendsMembershipFilter(t *testing.T) {
	target := newFakeTarget(t)
	all := []map[string]any{
		catalogFixture("plex", "media", false),
		catalogFixture("jellyfin", "media", true),
		catalogFixture("nextcloud", "productivity", false),
	}
	media := []map[string]any{all[0], all[1]}

	target.respondFunc("app.available", func(params json.RawMessage) any {
		var args []any
		_ = json.Unmarshal(params, &args)
		if len(args) == 1 {
			if filters, ok := args[0].([]any); ok {
				for _, raw := range filters {
					triple, ok := raw.([]any)
					if ok && len(triple) == 3 && triple[0] == "categories" &&
						triple[1] == "rin" && triple[2] == "media" {
						return media
					}
				}
			}
		}
		// A call that does not carry the expected filter gets the whole
		// catalog back, so a test that forgot to send the filter fails on
		// total (3) rather than passing by accident.
		return all
	})
	client := discoveryClient(t, discoverySession(t, target), false)

	res, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "catalog",
		Arguments: map[string]any{"op": "list", "category": "media"},
	})
	if err != nil {
		t.Fatalf("call catalog: %v", err)
	}
	if res.IsError {
		t.Fatalf("catalog returned an error: %v", res.Content)
	}

	out, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content, got %T", res.StructuredContent)
	}
	if total, _ := out["total"].(float64); total != 2 {
		t.Errorf("total = %v, want 2 -- the filtered count, not the unfiltered catalog's 3", out["total"])
	}
	if truncated, _ := out["truncated"].(bool); truncated {
		t.Error("truncated must be false: both matching entries fit under the default limit")
	}

	raw, ok := target.lastParams("app.available")
	if !ok {
		t.Fatal("app.available was never called")
	}
	if !strings.Contains(string(raw), `"rin"`) {
		t.Errorf("expected the membership operator to reach the target, got params %s", raw)
	}
}

// TestCatalogCategoriesReachesCaller is the end-to-end counterpart to
// TestCatalogCategoriesTakesNoArgument: it proves the vocabulary app.categories
// hands back actually reaches a caller through catalog(op="categories"), which
// is the entire point of the op -- a caller must not have to guess a category
// name and misread an empty list result as "nothing installed" rather than
// "wrong word".
func TestCatalogCategoriesReachesCaller(t *testing.T) {
	target := newFakeTarget(t)
	target.respond("app.categories", []string{"media", "productivity", "networking"})
	client := discoveryClient(t, discoverySession(t, target), false)

	res, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "catalog",
		Arguments: map[string]any{"op": "categories"},
	})
	if err != nil {
		t.Fatalf("call catalog: %v", err)
	}
	if res.IsError {
		t.Fatalf("catalog returned an error: %v", res.Content)
	}

	out, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content, got %T", res.StructuredContent)
	}

	items, ok := out["result"].([]any)
	if !ok {
		t.Fatalf("expected result to be a list, got %T", out["result"])
	}
	want := []string{"media", "productivity", "networking"}
	if len(items) != len(want) {
		t.Fatalf("result has %d items, want %d", len(items), len(want))
	}
	for i, v := range want {
		if items[i] != v {
			t.Errorf("result[%d] = %v, want %q", i, items[i], v)
		}
	}
}

// TestCatalogListFullReturnsCompleteRecords is the server-level counterpart to
// TestCatalogListProjectsIdentityAndVersionFields: that test proves the concern
// declares a projection, this one proves full=true actually bypasses it end to
// end -- app_readme and description, dropped by default, must survive when a
// caller explicitly asks for everything.
func TestCatalogListFullReturnsCompleteRecords(t *testing.T) {
	target := newFakeTarget(t)
	target.respond("app.available", []map[string]any{
		catalogFixture("plex", "media", false),
		catalogFixture("jellyfin", "media", true),
	})
	client := discoveryClient(t, discoverySession(t, target), false)

	res, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "catalog",
		Arguments: map[string]any{"op": "list", "full": true},
	})
	if err != nil {
		t.Fatalf("call catalog: %v", err)
	}
	if res.IsError {
		t.Fatalf("catalog returned an error: %v", res.Content)
	}

	out, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content, got %T", res.StructuredContent)
	}
	if projected, _ := out["projected"].(bool); projected {
		t.Error("projected must be false: full=true must bypass the projection entirely")
	}

	items, ok := out["result"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected 2 entries, got %#v", out["result"])
	}
	for _, raw := range items {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("expected item to be an object, got %T", raw)
		}
		if _, ok := entry["app_readme"]; !ok {
			t.Error("full=true must return app_readme -- the projection normally drops it")
		}
		if _, ok := entry["description"]; !ok {
			t.Error("full=true must return description -- the projection normally drops it")
		}
	}
}
