package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func validateReadSelection(concern string, in DispatchInput) error {
	if in.Offset != nil && (concern != "filesystem" || in.Op != "read") {
		return fmt.Errorf("offset applies only to filesystem read")
	}
	selected := len(in.Fields) > 0 || len(in.ExcludeFields) > 0
	if selected && (concern != "apps" || in.Op != "config") {
		return fmt.Errorf("fields and exclude_fields apply only to apps config")
	}
	if len(in.Fields) > 0 && len(in.ExcludeFields) > 0 || selected && in.Full {
		return fmt.Errorf("use only one of fields, exclude_fields, or full")
	}
	return nil
}

func configRevision(config map[string]any) (string, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

func appConfigResult(raw json.RawMessage, in DispatchInput) (*mcp.CallToolResult, DispatchOutput, error) {
	var config map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&config); err != nil || config == nil {
		return toolError("app.config did not return a configuration object"), DispatchOutput{}, nil
	}
	revision, err := configRevision(config)
	if err != nil {
		return nil, DispatchOutput{}, err
	}
	for _, fields := range [][]string{in.Fields, in.ExcludeFields} {
		for _, field := range fields {
			if _, exists := config[field]; !exists {
				return toolError(fmt.Sprintf("unknown app config field %q; fields must name top-level keys", field)), DispatchOutput{}, nil
			}
		}
	}
	total := len(config)
	if len(in.Fields) > 0 {
		selected := make(map[string]any, len(in.Fields))
		for _, field := range in.Fields {
			selected[field] = config[field]
		}
		config = selected
	}
	for _, field := range in.ExcludeFields {
		delete(config, field)
	}
	return nil, DispatchOutput{Op: in.Op, Revision: revision, Projected: len(config) < total, Result: config}, nil
}
