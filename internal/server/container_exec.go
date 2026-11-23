package server

import (
	"context"
	"time"

	"github.com/cedricziel/truenas-mcp/internal/truenas"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ContainerExecInput struct {
	App            string `json:"app" jsonschema:"installed Apps application name"`
	Container      string `json:"container" jsonschema:"exact container ID from apps containers"`
	Command        string `json:"command" jsonschema:"shell command executed using /bin/sh inside the selected container"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"wall time limit: default 30, maximum 120 seconds"`
	OutputLimit    int    `json:"output_limit,omitempty" jsonschema:"combined terminal output limit: default 16384, maximum 65536 bytes"`
}

func registerContainerExec(srv *mcp.Server, session sessionFor) {
	mcp.AddTool(srv, &mcp.Tool{Name: "container_exec", Description: "Execute a shell command in an Apps container as its default container user. Can modify mounted data. Returns combined terminal output and exit code when completion is observed. Timeout or truncation closes the terminal but does not guarantee remote processes stopped; do not automatically retry.", Annotations: writeAnnotations("Execute container command", true, false)}, func(ctx context.Context, _ *mcp.CallToolRequest, in ContainerExecInput) (*mcp.CallToolResult, truenas.ExecResult, error) {
		if in.TimeoutSeconds == 0 {
			in.TimeoutSeconds = 30
		}
		if in.OutputLimit == 0 {
			in.OutputLimit = 16384
		}
		if in.TimeoutSeconds < 1 || in.TimeoutSeconds > 120 || in.OutputLimit < 1 || in.OutputLimit > 65536 {
			return toolError("timeout_seconds must be 1–120 and output_limit must be 1–65536"), truenas.ExecResult{}, nil
		}
		s, err := session(ctx)
		if err != nil {
			return nil, truenas.ExecResult{}, err
		}
		result, err := s.Client().ContainerExec(ctx, in.App, in.Container, in.Command, time.Duration(in.TimeoutSeconds)*time.Second, in.OutputLimit)
		return nil, result, err
	})
}
