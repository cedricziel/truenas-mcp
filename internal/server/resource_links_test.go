package server

import "testing"

func TestJobWriteReturnsReadableResourceLink(t *testing.T) {
	target := newTaskTarget(t)
	s := taskServer(t, target)
	res, err := s.callTool("app_stop", map[string]any{"target": "paperless"}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertJobResourceLink(t, res)
	raw, wireErr := s.call("resources/read", map[string]any{"uri": "truenas://job/42", "_meta": metaFor(false)})
	if wireErr != nil {
		t.Fatal(wireErr)
	}
	contents := decodeObject(t, raw)["contents"].([]any)
	if contents[0].(map[string]any)["uri"] != "truenas://job/42" {
		t.Fatalf("linked resource unreadable: %s", raw)
	}
}

func TestDiscoveredJobReturnsResourceLink(t *testing.T) {
	target := newCaveatFakeTarget(t)
	s := newRawSession(t, NewMCPServer(MCPConfig{EnableWrites: true}, discoverySession(t, target)))
	res, err := s.callTool("call_method", map[string]any{"method": "app.update", "params": []any{"paperless", map[string]any{}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertJobResourceLink(t, res)
}

func TestSynchronousWriteDoesNotReturnJobLink(t *testing.T) {
	target := newFakeTarget(t)
	target.respond("pool.snapshot.create", map[string]any{"id": "tank/data@before"})
	s := newRawSession(t, NewMCPServer(MCPConfig{EnableWrites: true}, discoverySession(t, target)))
	res, err := s.callTool("create_snapshot", map[string]any{"target": "tank/data", "snapshot_name": "before"}, false)
	if err != nil || res["isError"] == true {
		t.Fatalf("snapshot: %v %v", res, err)
	}
	content := res["content"].([]any)
	if len(content) != 1 || content[0].(map[string]any)["type"] != "text" {
		t.Fatalf("synchronous content = %v", content)
	}
}

func TestConfigJobReturnsResourceLink(t *testing.T) {
	target := newFakeTarget(t)
	target.respond("core.get_methods", map[string]any{"filesystem.setacl": map[string]any{"job": true}})
	target.respond("filesystem.setacl", 42)
	s := newRawSession(t, NewMCPServer(MCPConfig{EnableWrites: true}, discoverySession(t, target)))
	res, err := s.callTool("set_path_acl", map[string]any{"config": map[string]any{"path": "/mnt/tank/data", "dacl": []any{}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertJobResourceLink(t, res)
}

func TestCompletedTaskReturnsResourceLink(t *testing.T) {
	target := newTaskTarget(t)
	s := taskServer(t, target)
	started, err := s.callTool("app_stop", map[string]any{"target": "paperless"}, true)
	if err != nil {
		t.Fatal(err)
	}
	target.setJob("SUCCESS", 100, "Stopped", "", nil)
	res, err := s.task("tasks/get", started["taskId"].(string))
	if err != nil {
		t.Fatal(err)
	}
	assertJobResourceLink(t, res["result"].(map[string]any))
}

func assertJobResourceLink(t *testing.T, res map[string]any) {
	t.Helper()
	content, _ := res["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("want text fallback and resource link; got %v", res)
	}
	if content[0].(map[string]any)["type"] != "text" || res["structuredContent"] == nil {
		t.Fatalf("lost existing result: %v", res)
	}
	link := content[1].(map[string]any)
	if link["type"] != "resource_link" || link["uri"] != "truenas://job/42" || link["mimeType"] != "application/json" || link["name"] == "" {
		t.Fatalf("job resource link = %v", link)
	}
}
