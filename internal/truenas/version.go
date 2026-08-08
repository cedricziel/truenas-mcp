package truenas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// ErrUnsupportedVersion means the target predates the versioned JSON-RPC API.
var ErrUnsupportedVersion = errors.New("unsupported TrueNAS version")

// Minimum supported release. The REST API was deprecated in 25.04 and the
// versioned JSON-RPC API became the supported path; earlier releases are not
// targeted.
const (
	minMajor = 25
	minMinor = 4
)

// Version is a parsed TrueNAS release.
type Version struct {
	Raw   string `json:"raw"`
	Major int    `json:"major"`
	Minor int    `json:"minor"`
}

var versionPattern = regexp.MustCompile(`TrueNAS-(\d+)\.(\d+)`)

// CheckVersion reads the target's release and refuses to proceed below the
// supported floor.
//
// An unrecognised version string is not fatal: it is far more likely to mean
// the format changed than that the box is ancient, and refusing to start on a
// parsing disagreement would be worse than proceeding.
func (c *Client) CheckVersion(ctx context.Context) (Version, error) {
	raw, err := c.Call(ctx, "system.version")
	if err != nil {
		return Version{}, err
	}

	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return Version{}, fmt.Errorf("decoding version: %w", err)
	}

	v := Version{Raw: s}
	m := versionPattern.FindStringSubmatch(s)
	if m == nil {
		return v, nil
	}

	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])

	if v.Major < minMajor || (v.Major == minMajor && v.Minor < minMinor) {
		return v, fmt.Errorf("%w: target runs %d.%02d, minimum supported is %d.%02d",
			ErrUnsupportedVersion, v.Major, v.Minor, minMajor, minMinor)
	}
	return v, nil
}
