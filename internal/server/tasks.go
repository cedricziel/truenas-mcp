package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cedricziel/truenas-mcp/internal/truenas"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP Tasks (the io.modelcontextprotocol/tasks extension, stable as of the
// 2026-07-28 protocol revision) is the protocol's own shape for what every
// mutating tool here already does: hand back a handle and let the caller
// poll. A TrueNAS job is a task in all but name, so this file maps one onto
// the other rather than inventing a second lifecycle.
//
// The extension is server-directed. A client that declares it in the
// capabilities it sends with each request may receive, in place of a tool's
// ordinary result, a CreateTaskResult carrying a task id; it then polls
// tasks/get, which reports the job's state and, once terminal, the result the
// tool would have produced synchronously. A client that does not declare it
// gets the job-started result it always did, so nothing changes for a host
// without the extension.
//
// The go-sdk models none of this. tasks/get, tasks/update, and tasks/cancel
// are registered as custom methods, and the tools/call substitution is a
// receiving middleware, because a tool handler can only return a
// CallToolResult and the SDK owns that type's wire shape.
//
// Task ids are minted here and mapped to job ids in a per-server registry.
// A server is built per credential, so a task is reachable only under the
// credential that created it, which is the authorization binding the
// extension requires; the registry also means tasks/get cannot be used to
// watch an arbitrary job by guessing its number, though the jobs tool allows
// exactly that under the same key. The registry lives in memory: after a
// restart a task id is unknown and tasks/get says so, and the client falls
// back to the job id the ordinary result carries in its resource URI.

// TasksExtensionID is the extension identifier both sides negotiate.
const TasksExtensionID = "io.modelcontextprotocol/tasks"

const (
	methodTaskGet    = "tasks/get"
	methodTaskUpdate = "tasks/update"
	methodTaskCancel = "tasks/cancel"

	// taskTTL is how long a finished task is retrievable. Middleware jobs
	// are pruned by the target on its own schedule; this bounds the
	// registry rather than the job.
	taskTTL = time.Hour

	// taskPollInterval is the suggested time between tasks/get calls. App
	// upgrades take tens of seconds; a snapshot takes under one. Two seconds
	// keeps a fast job from feeling slow without polling a slow one hard.
	taskPollInterval = 2 * time.Second
)

// Task status values, as the extension names them.
const (
	taskWorking   = "working"
	taskCompleted = "completed"
	taskFailed    = "failed"
	taskCancelled = "cancelled"
)

// taskEntry is what the registry remembers about one task.
type taskEntry struct {
	JobID     int64
	Tool      string
	Method    string
	Target    string
	CreatedAt time.Time
}

// taskRegistry maps the task ids this server minted to the jobs behind them.
type taskRegistry struct {
	mu    sync.Mutex
	tasks map[string]taskEntry
	now   func() time.Time
}

func newTaskRegistry() *taskRegistry {
	return &taskRegistry{tasks: map[string]taskEntry{}, now: time.Now}
}

// add mints an id for a job. Ids are random rather than derived from the job
// number so that a task id conveys nothing about the target's job table.
func (r *taskRegistry) add(entry taskEntry) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("tasks: reading random bytes: %v", err))
	}
	id := hex.EncodeToString(b[:])

	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune()
	entry.CreatedAt = r.now()
	r.tasks[id] = entry
	return id
}

func (r *taskRegistry) get(id string) (taskEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune()
	entry, ok := r.tasks[id]
	return entry, ok
}

// prune forgets tasks past their TTL. Called under mu.
func (r *taskRegistry) prune() {
	cutoff := r.now().Add(-taskTTL)
	for id, entry := range r.tasks {
		if entry.CreatedAt.Before(cutoff) {
			delete(r.tasks, id)
		}
	}
}

// task is the extension's Task object.
type task struct {
	TaskID         string `json:"taskId"`
	Status         string `json:"status"`
	StatusMessage  string `json:"statusMessage,omitempty"`
	CreatedAt      string `json:"createdAt"`
	LastUpdatedAt  string `json:"lastUpdatedAt"`
	TTLMs          int64  `json:"ttlMs"`
	PollIntervalMs int64  `json:"pollIntervalMs,omitempty"`
}

func newTask(id string, entry taskEntry, now time.Time) task {
	return task{
		TaskID:         id,
		Status:         taskWorking,
		CreatedAt:      entry.CreatedAt.UTC().Format(time.RFC3339),
		LastUpdatedAt:  now.UTC().Format(time.RFC3339),
		TTLMs:          taskTTL.Milliseconds(),
		PollIntervalMs: taskPollInterval.Milliseconds(),
	}
}

// createTaskResult is the CreateTaskResult a tools/call returns in place of
// its ordinary result. It embeds the result it replaces so the SDK still sees
// a Result it knows how to post-process (server info in _meta, the
// resultType stamp -- which the marshaller below overrides, since the whole
// point is that this result's type is "task"); the wire shape is entirely
// this type's own.
type createTaskResult struct {
	*mcp.CallToolResult
	task task
}

func (r *createTaskResult) MarshalJSON() ([]byte, error) {
	type wire struct {
		Meta       map[string]any `json:"_meta,omitempty"`
		ResultType string         `json:"resultType"`
		task
	}
	return json.Marshal(wire{Meta: r.GetMeta(), ResultType: "task", task: r.task})
}

// jobStarted is the shape both write tiers produce when they start a job:
// write tools put job_id at the top level, config writes nest it under
// result. Either way the tool's answer was "started, follow this job".
func jobStarted(res *mcp.CallToolResult) (jobID int64, method, target string, ok bool) {
	if res == nil || res.IsError || res.StructuredContent == nil {
		return 0, "", "", false
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return 0, "", "", false
	}
	var out struct {
		JobID  int64  `json:"job_id"`
		Method string `json:"method"`
		Target string `json:"target"`
		Result struct {
			JobID int64 `json:"job_id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return 0, "", "", false
	}
	switch {
	case out.JobID > 0:
		return out.JobID, out.Method, out.Target, true
	case out.Result.JobID > 0:
		return out.Result.JobID, out.Method, out.Target, true
	}
	return 0, "", "", false
}

// clientDeclaresTasks reports whether the calling client opted into the
// extension on this request. Under the 2026-07-28 protocol that is per
// request; for an older client it falls back to what initialize carried,
// which is what ClientCapabilities already does.
func clientDeclaresTasks(req mcp.Request) bool {
	r, ok := req.(interface {
		ClientCapabilities() *mcp.ClientCapabilities
	})
	if !ok {
		return false
	}
	caps := r.ClientCapabilities()
	if caps == nil {
		return false
	}
	_, declared := caps.Extensions[TasksExtensionID]
	return declared
}

// registerTasks wires the extension: the capability, the substitution
// middleware, and the three methods. Only a server with the write tier ever
// starts a job, so only that server carries the extension -- the same rule
// that keeps a mutating tool off the tool list when the tier is disabled.
func registerTasks(srv *mcp.Server, session sessionFor, registry *taskRegistry) {
	srv.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			res, err := next(ctx, method, req)
			if err != nil || method != "tools/call" || !clientDeclaresTasks(req) {
				return res, err
			}
			callRes, ok := res.(*mcp.CallToolResult)
			if !ok {
				return res, nil
			}
			jobID, jobMethod, target, ok := jobStarted(callRes)
			if !ok {
				return res, nil
			}
			tool := ""
			if p, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok {
				tool = p.Name
			}
			entry := taskEntry{JobID: jobID, Tool: tool, Method: jobMethod, Target: target}
			id := registry.add(entry)
			entry, _ = registry.get(id)
			t := newTask(id, entry, registry.now())
			t.StatusMessage = fmt.Sprintf("%s started job %d on the target", jobMethod, jobID)
			return &createTaskResult{CallToolResult: callRes, task: t}, nil
		}
	})

	must := func(err error) {
		if err != nil {
			panic(err) // a standard method name; caught by every test that builds a server
		}
	}

	must(mcp.AddReceivingCustomMethod(srv, methodTaskGet,
		func(ctx context.Context, _ *mcp.ServerSession, params *taskParams) (*getTaskResult, error) {
			id, entry, err := lookupTask(registry, params)
			if err != nil {
				return nil, err
			}
			s, err := session(ctx)
			if err != nil {
				return nil, err
			}
			job, err := s.Client().Job(ctx, entry.JobID)
			if err != nil {
				if errors.Is(err, truenas.ErrJobNotFound) {
					return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams,
						Message: fmt.Sprintf("task %s has expired: the target no longer has job %d", id, entry.JobID)}
				}
				return nil, err
			}
			return describeTask(id, entry, job, registry.now()), nil
		}))

	must(mcp.AddReceivingCustomMethod(srv, methodTaskUpdate,
		func(_ context.Context, _ *mcp.ServerSession, params *updateTaskParams) (*emptyTaskResult, error) {
			// No task here ever asks for input, so there is nothing to apply;
			// the extension says unknown or already-satisfied keys are ignored.
			if _, _, err := lookupTask(registry, &params.taskParams); err != nil {
				return nil, err
			}
			return &emptyTaskResult{completeResult: complete()}, nil
		}))

	must(mcp.AddReceivingCustomMethod(srv, methodTaskCancel,
		func(ctx context.Context, _ *mcp.ServerSession, params *taskParams) (*emptyTaskResult, error) {
			_, entry, err := lookupTask(registry, params)
			if err != nil {
				return nil, err
			}
			s, err := session(ctx)
			if err != nil {
				return nil, err
			}
			// Cancellation is cooperative: the target is asked to abort and
			// the request is acknowledged whether or not the job can still
			// stop. A job already finished cannot be aborted and the target
			// says so; that is not a failure of the cancel request.
			if _, err := s.Client().Call(ctx, "core.job_abort", entry.JobID); err != nil {
				var callErr *truenas.CallError
				if !errors.As(err, &callErr) {
					return nil, err
				}
			}
			return &emptyTaskResult{completeResult: complete()}, nil
		}))
}

type taskParams struct {
	mcp.ParamsBase
	TaskID string `json:"taskId"`
}

type updateTaskParams struct {
	taskParams
	InputResponses map[string]any `json:"inputResponses,omitempty"`
}

// Every result under the 2026-07-28 protocol carries a resultType. The SDK
// stamps "complete" onto its own result types after a handler returns, but
// not onto a custom method's, so these carry it themselves.
type completeResult struct {
	ResultType string `json:"resultType"`
}

func complete() completeResult { return completeResult{ResultType: "complete"} }

type emptyTaskResult struct {
	mcp.ResultBase
	completeResult
}

// getTaskResult is the extension's GetTaskResult: the task, plus its outcome
// once it has one.
type getTaskResult struct {
	mcp.ResultBase
	completeResult
	task
	Result *mcp.CallToolResult `json:"result,omitempty"`
	Error  *jsonrpc.Error      `json:"error,omitempty"`
}

func lookupTask(registry *taskRegistry, params *taskParams) (string, taskEntry, error) {
	if params == nil || strings.TrimSpace(params.TaskID) == "" {
		return "", taskEntry{}, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "taskId is required"}
	}
	entry, ok := registry.get(params.TaskID)
	if !ok {
		return "", taskEntry{}, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams,
			Message: fmt.Sprintf("task %s not found; it may have expired or belong to another session", params.TaskID)}
	}
	return params.TaskID, entry, nil
}

// describeTask maps a middleware job onto the task lifecycle.
func describeTask(id string, entry taskEntry, job truenas.Job, now time.Time) *getTaskResult {
	res := &getTaskResult{completeResult: complete(), task: newTask(id, entry, now)}
	switch job.State {
	case "SUCCESS":
		res.Status = taskCompleted
		res.PollIntervalMs = 0
		res.StatusMessage = fmt.Sprintf("%s finished", entry.Method)
		res.Result = jobCompletedResult(entry, job)
	case "FAILED":
		res.Status = taskFailed
		res.PollIntervalMs = 0
		res.StatusMessage = job.Error
		res.Error = &jsonrpc.Error{Code: jsonrpc.CodeInternalError,
			Message: fmt.Sprintf("%s failed on the target: %s", entry.Method, job.Error)}
	case "ABORTED":
		res.Status = taskCancelled
		res.PollIntervalMs = 0
		res.StatusMessage = fmt.Sprintf("%s was aborted on the target", entry.Method)
	default:
		res.Status = taskWorking
		msg := job.Description
		if job.Percent > 0 {
			msg = strings.TrimSpace(fmt.Sprintf("%s (%.0f%%)", job.Description, job.Percent))
		}
		res.StatusMessage = msg
	}
	return res
}

// JobCompletedOutput is what a task's completed result carries: the same
// identity the job-started result had, plus the job's outcome, which a
// synchronous call would have waited for.
type JobCompletedOutput struct {
	JobID  int64  `json:"job_id"`
	Method string `json:"method"`
	Target string `json:"target,omitempty"`
	State  string `json:"state"`
	Result any    `json:"result,omitempty"`
}

func jobCompletedResult(entry taskEntry, job truenas.Job) *mcp.CallToolResult {
	out := JobCompletedOutput{JobID: job.ID, Method: entry.Method, Target: entry.Target, State: job.State, Result: job.Result}
	text, _ := json.Marshal(out)
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(text)}},
		StructuredContent: out,
	}
}
