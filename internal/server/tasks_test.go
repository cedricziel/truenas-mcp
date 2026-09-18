package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The go-sdk client decodes every tools/call answer into a CallToolResult,
// which would silently drop the task fields these tests exist to check. So
// they speak raw JSON-RPC over an in-memory transport and inspect the bytes
// a real client receives.

// rawSession is a raw JSON-RPC client bound to one server.
type rawSession struct {
	t    *testing.T
	conn mcp.Connection
	next int
}

func newRawSession(t *testing.T, srv *mcp.Server) *rawSession {
	t.Helper()
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	conn, err := ct.Connect(context.Background())
	if err != nil {
		t.Fatalf("connect client transport: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &rawSession{t: t, conn: conn}
}

// metaFor builds the per-request _meta a 2026-07-28 client sends, declaring
// the tasks extension or not.
func metaFor(tasks bool) map[string]any {
	caps := map[string]any{}
	if tasks {
		caps["extensions"] = map[string]any{TasksExtensionID: map[string]any{}}
	}
	return map[string]any{
		"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
		"io.modelcontextprotocol/clientCapabilities": caps,
	}
}

// call sends one request and returns the raw result or the wire error.
func (s *rawSession) call(method string, params map[string]any) (json.RawMessage, *jsonrpc.Error) {
	s.t.Helper()
	s.next++
	id, _ := jsonrpc.MakeID(fmt.Sprint(s.next))
	raw, err := json.Marshal(params)
	if err != nil {
		s.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.conn.Write(ctx, &jsonrpc.Request{ID: id, Method: method, Params: raw}); err != nil {
		s.t.Fatalf("write %s: %v", method, err)
	}
	for {
		msg, err := s.conn.Read(ctx)
		if err != nil {
			s.t.Fatalf("read reply to %s: %v", method, err)
		}
		res, ok := msg.(*jsonrpc.Response)
		if !ok {
			continue // a notification; not what we are waiting for
		}
		if res.Error != nil {
			wireErr, ok := res.Error.(*jsonrpc.Error)
			if !ok {
				s.t.Fatalf("%s: non-wire error %v", method, res.Error)
			}
			return nil, wireErr
		}
		return res.Result, nil
	}
}

func (s *rawSession) callTool(name string, args map[string]any, tasks bool) (map[string]any, *jsonrpc.Error) {
	s.t.Helper()
	raw, wireErr := s.call("tools/call", map[string]any{"name": name, "arguments": args, "_meta": metaFor(tasks)})
	if wireErr != nil {
		return nil, wireErr
	}
	return decodeObject(s.t, raw), nil
}

func (s *rawSession) task(method, taskID string) (map[string]any, *jsonrpc.Error) {
	s.t.Helper()
	raw, wireErr := s.call(method, map[string]any{"taskId": taskID, "_meta": metaFor(true)})
	if wireErr != nil {
		return nil, wireErr
	}
	return decodeObject(s.t, raw), nil
}

func decodeObject(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("result is not an object: %s", raw)
	}
	return out
}

// taskTarget is a fake box with one app whose stop starts job 42, and a
// job table the test moves through the lifecycle.
type taskTarget struct {
	*fakeTarget
	job map[string]any
}

func newTaskTarget(t *testing.T) *taskTarget {
	t.Helper()
	target := &taskTarget{fakeTarget: newFakeTarget(t)}
	target.respond("app.query", []map[string]any{{"name": "paperless", "state": "RUNNING"}})
	target.respond("app.stop", 42)
	target.respond("core.job_abort", true)
	target.setJob("RUNNING", 30, "Stopping containers", "", nil)
	target.respondFunc("core.get_jobs", func(json.RawMessage) any {
		target.mu.Lock()
		defer target.mu.Unlock()
		if target.job == nil {
			return []any{}
		}
		return []any{target.job}
	})
	return target
}

func (target *taskTarget) setJob(state string, percent float64, description, jobErr string, result any) {
	target.mu.Lock()
	defer target.mu.Unlock()
	target.job = map[string]any{
		"id": 42, "method": "app.stop", "state": state, "error": jobErr, "result": result,
		"progress": map[string]any{"percent": percent, "description": description},
	}
}

func (target *taskTarget) pruneJob() {
	target.mu.Lock()
	defer target.mu.Unlock()
	target.job = nil
}

func taskServer(t *testing.T, target *taskTarget) *rawSession {
	t.Helper()
	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: true}, discoverySession(t, target.fakeTarget))
	return newRawSession(t, srv)
}

func startTask(t *testing.T, s *rawSession) string {
	t.Helper()
	res, wireErr := s.callTool("app_stop", map[string]any{"target": "paperless"}, true)
	if wireErr != nil {
		t.Fatalf("app_stop: %v", wireErr)
	}
	if res["resultType"] != "task" {
		t.Fatalf("expected a task result, got %v", res)
	}
	id, _ := res["taskId"].(string)
	if id == "" {
		t.Fatalf("task result carries no taskId: %v", res)
	}
	return id
}

// The extension is advertised exactly where a job can be started.
func TestTasksExtensionAdvertisedOnlyWithWrites(t *testing.T) {
	cases := map[string]struct {
		cfg     MCPConfig
		session sessionFor
		want    bool
	}{
		"writes with a session":    {MCPConfig{Version: "t", EnableWrites: true}, stubSession, true},
		"read-only":                {MCPConfig{Version: "t"}, stubSession, false},
		"writes without a session": {MCPConfig{Version: "t", EnableWrites: true}, nil, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := newRawSession(t, NewMCPServer(tc.cfg, tc.session))
			raw, wireErr := s.call("server/discover", map[string]any{"_meta": metaFor(true)})
			if wireErr != nil {
				t.Fatalf("server/discover: %v", wireErr)
			}
			var discover struct {
				Capabilities struct {
					Extensions map[string]any `json:"extensions"`
				} `json:"capabilities"`
			}
			if err := json.Unmarshal(raw, &discover); err != nil {
				t.Fatal(err)
			}
			_, got := discover.Capabilities.Extensions[TasksExtensionID]
			if got != tc.want {
				t.Errorf("tasks extension advertised = %v, want %v (extensions: %v)", got, tc.want, discover.Capabilities.Extensions)
			}
		})
	}
}

// A client that did not opt in sees exactly what it always saw.
func TestWriteWithoutTasksCapabilityReturnsJobStarted(t *testing.T) {
	s := taskServer(t, newTaskTarget(t))
	res, wireErr := s.callTool("app_stop", map[string]any{"target": "paperless"}, false)
	if wireErr != nil {
		t.Fatalf("app_stop: %v", wireErr)
	}
	if res["resultType"] != "complete" {
		t.Errorf("resultType = %v, want complete", res["resultType"])
	}
	if _, leaked := res["taskId"]; leaked {
		t.Error("a client that did not declare the extension must never receive a task")
	}
	sc, _ := res["structuredContent"].(map[string]any)
	if sc["job_id"] != float64(42) {
		t.Errorf("structuredContent = %v, want job_id 42", sc)
	}
}

func TestWriteWithTasksCapabilityReturnsCreateTaskResult(t *testing.T) {
	s := taskServer(t, newTaskTarget(t))
	res, wireErr := s.callTool("app_stop", map[string]any{"target": "paperless"}, true)
	if wireErr != nil {
		t.Fatalf("app_stop: %v", wireErr)
	}
	if res["resultType"] != "task" || res["status"] != taskWorking {
		t.Errorf("result = %v, want resultType task and status working", res)
	}
	for _, key := range []string{"taskId", "createdAt", "lastUpdatedAt", "ttlMs", "pollIntervalMs"} {
		if _, ok := res[key]; !ok {
			t.Errorf("task result lacks %s: %v", key, res)
		}
	}
	if _, ok := res["content"]; ok {
		t.Error("a CreateTaskResult carries no content; the result arrives through tasks/get")
	}
	meta, _ := res["_meta"].(map[string]any)
	if _, ok := meta["io.modelcontextprotocol/serverInfo"]; !ok {
		t.Errorf("the SDK's server identification must survive the substitution: %v", res["_meta"])
	}
	if msg, _ := res["statusMessage"].(string); msg == "" {
		t.Error("a status message naming the job helps a host show something before the first poll")
	}
}

// A tool that does not start a job is untouched, extension or not.
func TestReadToolIsNeverTurnedIntoATask(t *testing.T) {
	s := taskServer(t, newTaskTarget(t))
	res, wireErr := s.callTool("server_info", map[string]any{}, true)
	if wireErr != nil {
		t.Fatalf("server_info: %v", wireErr)
	}
	if res["resultType"] != "complete" {
		t.Errorf("resultType = %v, want complete", res["resultType"])
	}
}

func TestTaskFollowsTheJobToCompletion(t *testing.T) {
	target := newTaskTarget(t)
	s := taskServer(t, target)
	id := startTask(t, s)

	got, wireErr := s.task(methodTaskGet, id)
	if wireErr != nil {
		t.Fatalf("tasks/get: %v", wireErr)
	}
	if got["status"] != taskWorking || got["statusMessage"] != "Stopping containers (30%)" {
		t.Errorf("working task = %v", got)
	}
	if got["resultType"] != "complete" {
		t.Errorf("a tasks/get answer is itself a complete result: %v", got["resultType"])
	}
	if _, early := got["result"]; early {
		t.Error("no result before the job finishes")
	}

	target.setJob("SUCCESS", 100, "Done", "", map[string]any{"stopped": true})
	got, wireErr = s.task(methodTaskGet, id)
	if wireErr != nil {
		t.Fatalf("tasks/get: %v", wireErr)
	}
	if got["status"] != taskCompleted {
		t.Fatalf("finished task = %v", got)
	}
	result, _ := got["result"].(map[string]any)
	sc, _ := result["structuredContent"].(map[string]any)
	if sc["state"] != "SUCCESS" || sc["job_id"] != float64(42) || sc["method"] != "app.stop" || sc["target"] != "paperless" {
		t.Errorf("completed result = %v", result)
	}
	if inner, _ := sc["result"].(map[string]any); inner["stopped"] != true {
		t.Errorf("the job's own return value should be carried: %v", sc["result"])
	}
	if _, ok := got["pollIntervalMs"]; ok {
		t.Error("a terminal task suggests no further polling")
	}
}

func TestFailedJobIsAFailedTask(t *testing.T) {
	target := newTaskTarget(t)
	s := taskServer(t, target)
	id := startTask(t, s)
	target.setJob("FAILED", 50, "", "container refused to stop", nil)

	got, wireErr := s.task(methodTaskGet, id)
	if wireErr != nil {
		t.Fatalf("tasks/get: %v", wireErr)
	}
	if got["status"] != taskFailed {
		t.Fatalf("task = %v, want failed", got)
	}
	jobErr, _ := got["error"].(map[string]any)
	if jobErr["code"] == nil {
		t.Errorf("failed task carries no JSON-RPC error: %v", got)
	}
	if msg, _ := jobErr["message"].(string); msg != "app.stop failed on the target: container refused to stop" {
		t.Errorf("error message = %q", msg)
	}
	if _, ok := got["result"]; ok {
		t.Error("a failed task has an error, not a result")
	}
}

func TestAbortedJobIsACancelledTask(t *testing.T) {
	target := newTaskTarget(t)
	s := taskServer(t, target)
	id := startTask(t, s)
	target.setJob("ABORTED", 50, "", "", nil)

	got, wireErr := s.task(methodTaskGet, id)
	if wireErr != nil {
		t.Fatalf("tasks/get: %v", wireErr)
	}
	if got["status"] != taskCancelled {
		t.Errorf("task = %v, want cancelled", got)
	}
}

func TestTaskLookupFailuresAreInvalidParams(t *testing.T) {
	target := newTaskTarget(t)
	s := taskServer(t, target)

	if _, wireErr := s.task(methodTaskGet, "no-such-task"); wireErr == nil || wireErr.Code != jsonrpc.CodeInvalidParams {
		t.Errorf("unknown task: %v, want code %d", wireErr, jsonrpc.CodeInvalidParams)
	}
	if _, wireErr := s.task(methodTaskGet, ""); wireErr == nil || wireErr.Code != jsonrpc.CodeInvalidParams {
		t.Errorf("missing taskId: %v, want code %d", wireErr, jsonrpc.CodeInvalidParams)
	}

	// The target pruning the job is the task expiring.
	id := startTask(t, s)
	target.pruneJob()
	_, wireErr := s.task(methodTaskGet, id)
	if wireErr == nil || wireErr.Code != jsonrpc.CodeInvalidParams {
		t.Fatalf("pruned job: %v, want code %d", wireErr, jsonrpc.CodeInvalidParams)
	}
	if !strings.Contains(wireErr.Message, "expired") {
		t.Errorf("message = %q, want it to say the task expired", wireErr.Message)
	}
}

func TestCancelAsksTheTargetToAbortTheJob(t *testing.T) {
	target := newTaskTarget(t)
	s := taskServer(t, target)
	id := startTask(t, s)

	got, wireErr := s.task(methodTaskCancel, id)
	if wireErr != nil {
		t.Fatalf("tasks/cancel: %v", wireErr)
	}
	if got["resultType"] != "complete" {
		t.Errorf("cancel result = %v", got)
	}
	params, ok := target.lastParams("core.job_abort")
	if !ok {
		t.Fatal("core.job_abort was never called")
	}
	if string(params) != "[42]" {
		t.Errorf("core.job_abort params = %s, want [42]", params)
	}

	// A job already finished cannot be aborted; the target's refusal is not
	// the cancel request failing.
	target.fail("core.job_abort", -32001, "job is not running")
	if _, wireErr := s.task(methodTaskCancel, id); wireErr != nil {
		t.Errorf("cancel after the job finished should still be acknowledged: %v", wireErr)
	}

	if _, wireErr := s.task(methodTaskCancel, "no-such-task"); wireErr == nil || wireErr.Code != jsonrpc.CodeInvalidParams {
		t.Errorf("cancelling an unknown task: %v", wireErr)
	}
}

func TestUpdateAcknowledgesKnownTasksOnly(t *testing.T) {
	s := taskServer(t, newTaskTarget(t))
	id := startTask(t, s)

	raw, wireErr := s.call(methodTaskUpdate, map[string]any{
		"taskId": id, "inputResponses": map[string]any{"anything": "ignored"}, "_meta": metaFor(true),
	})
	if wireErr != nil {
		t.Fatalf("tasks/update: %v", wireErr)
	}
	if decodeObject(t, raw)["resultType"] != "complete" {
		t.Errorf("update result = %s", raw)
	}
	if _, wireErr := s.task(methodTaskUpdate, "no-such-task"); wireErr == nil || wireErr.Code != jsonrpc.CodeInvalidParams {
		t.Errorf("updating an unknown task: %v", wireErr)
	}
}

// Tasks are minted per server, and a server is built per credential, so a
// task id from one credential is unknown under another.
func TestTasksAreBoundToTheServerThatMintedThem(t *testing.T) {
	target := newTaskTarget(t)
	first := taskServer(t, target)
	second := taskServer(t, target)
	id := startTask(t, first)

	if _, wireErr := second.task(methodTaskGet, id); wireErr == nil || wireErr.Code != jsonrpc.CodeInvalidParams {
		t.Errorf("another server resolved a task it did not mint: %v", wireErr)
	}
}

func TestTaskRegistryForgetsExpiredTasks(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	registry := newTaskRegistry()
	registry.now = func() time.Time { return now }

	id := registry.add(taskEntry{JobID: 1})
	if _, ok := registry.get(id); !ok {
		t.Fatal("a fresh task must be retrievable")
	}
	now = now.Add(taskTTL + time.Second)
	if _, ok := registry.get(id); ok {
		t.Error("a task past its TTL must be forgotten")
	}
}

func TestTaskIDsAreUnpredictable(t *testing.T) {
	registry := newTaskRegistry()
	a := registry.add(taskEntry{JobID: 7})
	b := registry.add(taskEntry{JobID: 7})
	if a == b {
		t.Error("two tasks for the same job must not share an id")
	}
	if len(a) != 32 {
		t.Errorf("task id %q should be 16 random bytes in hex, not the job number", a)
	}
}

// Both write tiers announce a started job, in two shapes.
func TestJobStartedRecognisesBothWriteShapes(t *testing.T) {
	cases := map[string]struct {
		content any
		want    int64
	}{
		"write tool":   {JobStartedOutput{JobID: 42, Method: "app.stop", Target: "paperless"}, 42},
		"config write": {ConfigWriteOutput{Method: "smb.update", Result: map[string]any{"job_id": 7}}, 7},
		"sync config":  {ConfigWriteOutput{Method: "smb.update", Result: map[string]any{"enabled": true}}, 0},
		"read":         {JobsOutput{Op: "list"}, 0},
	}
	for name, tc := range cases {
		id, _, _, ok := jobStarted(&mcp.CallToolResult{StructuredContent: tc.content})
		if (tc.want != 0) != ok || id != tc.want {
			t.Errorf("%s: jobStarted = %d, %v; want %d", name, id, ok, tc.want)
		}
	}
	if _, _, _, ok := jobStarted(&mcp.CallToolResult{IsError: true, StructuredContent: JobStartedOutput{JobID: 1}}); ok {
		t.Error("an error result never started a job")
	}
}
