package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type appConfigUpdateInput struct {
	Target           string         `json:"target" jsonschema:"the exact installed app name"`
	Changes          map[string]any `json:"changes" jsonschema:"nonempty partial configuration; objects merge recursively; arrays scalars and null replace fields"`
	ExpectedRevision string         `json:"expected_revision" jsonschema:"revision returned by apps(op=config) for the configuration being edited"`
}

func registerAppConfigUpdate(srv *mcp.Server, session sessionFor) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "app_update_config",
		Description: "Update an installed app's configuration by merging changes into its complete server-side configuration. Read apps(op=config) first and supply its revision. Objects merge recursively; arrays, scalars, and null replace fields. Top-level ix_context edits are forbidden. The revision check is optimistic and is not atomic with external writers. " + asyncContractNote,
		Annotations: writeAnnotations("Update app configuration", true, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in appConfigUpdateInput) (*mcp.CallToolResult, JobStartedOutput, error) {
		// The SDK's map decoding rounds integers above 2^53; preserve the original JSON numbers.
		inputDecoder := json.NewDecoder(bytes.NewReader(req.Params.Arguments))
		inputDecoder.UseNumber()
		in = appConfigUpdateInput{}
		if err := inputDecoder.Decode(&in); err != nil {
			return toolError("invalid configuration update arguments"), JobStartedOutput{}, nil
		}
		if strings.TrimSpace(in.Target) == "" || len(in.Changes) == 0 || strings.TrimSpace(in.ExpectedRevision) == "" {
			return toolError("target, nonempty changes, and expected_revision are required"), JobStartedOutput{}, nil
		}
		if _, exists := in.Changes["ix_context"]; exists {
			return toolError("changes must not edit top-level ix_context"), JobStartedOutput{}, nil
		}
		s, err := session(ctx)
		if err != nil {
			return nil, JobStartedOutput{}, err
		}
		raw, err := s.Client().Call(ctx, "app.config", in.Target)
		if err != nil {
			return nil, JobStartedOutput{}, err
		}
		var config map[string]any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&config); err != nil || config == nil {
			return toolError("app.config must return a configuration object"), JobStartedOutput{}, nil
		}
		revision, err := configRevision(config)
		if err != nil {
			return nil, JobStartedOutput{}, err
		}
		if revision != in.ExpectedRevision {
			return toolError("configuration revision changed; read apps(op=config) again before updating"), JobStartedOutput{}, nil
		}
		mergeAppConfig(config, in.Changes)
		jobID, err := s.Client().CallJob(ctx, "app.update", in.Target, map[string]any{"values": config})
		if err != nil {
			return nil, JobStartedOutput{}, err
		}
		return nil, JobStartedOutput{JobID: jobID, Method: "app.update", Target: in.Target, Resource: fmt.Sprintf("truenas://job/%d", jobID), Note: "Started; not yet finished. Follow it with jobs(op=\"show\", job_id=...)."}, nil
	})
}

func mergeAppConfig(config, changes map[string]any) {
	for key, value := range changes {
		changeObject, isObject := value.(map[string]any)
		existingObject, hasObject := config[key].(map[string]any)
		if isObject && hasObject {
			mergeAppConfig(existingObject, changeObject)
		} else {
			config[key] = value
		}
	}
}
