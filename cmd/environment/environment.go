// Copyright © 2026, SAS Institute Inc., Cary, NC, USA.  All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0
package environment

import (
	"bytes"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/sassoftware/viyactl/cmd"
)

// IsViyaDeploymentUp pings a deployment's /SASLogon to check if the deployment is up
func IsViyaDeploymentUp(env string) error {
	req, err := http.NewRequest(http.MethodPost, env, nil)
	if err != nil {
		return fmt.Errorf("unable to create http request")
	}

	resp, err := cmd.Client.Do(req)
	if err != nil {
		return fmt.Errorf("unable to access %q, is the SAS Viya deployment up?", fmt.Sprintf("%s/SASLogon", env))
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("expected %q to return 200, got %d, is the SAS Viya deployment up?", fmt.Sprintf("%s/SASLogon", env), resp.StatusCode)
	}
	return nil
}

// PatchBuffer fixes multi-line comments and is necessary until https://github.com/goccy/go-yaml/pull/759 is merged
func PatchBuffer(buf *bytes.Buffer) *bytes.Buffer {
	lines := strings.Split(buf.String(), "\n")
	var patched []string

	re := regexp.MustCompile(`.*: \|$`)
	i := 0
	changed := false

	for i < len(lines) {
		line := lines[i]
		patched = append(patched, line)

		if re.MatchString(line) && i+1 < len(lines) {
			nextLine := lines[i+1]
			trimmed := strings.TrimSpace(nextLine)

			if strings.HasPrefix(trimmed, "/*") {
				indent := leadingWhitespace(line) + "    " // base indent for YAML block
				i++                                        // move to comment block start

				for i < len(lines) {
					commentLine := lines[i]
					trimmedComment := strings.TrimSpace(commentLine)
					patched = append(patched, indent+trimmedComment)
					changed = true
					if strings.Contains(trimmedComment, "*/") {
						i++
						break
					}
					i++
				}
				continue // skip incrementing i again
			}
		}
		i++
	}

	if changed {
		return bytes.NewBufferString(strings.Join(patched, "\n"))
	}
	return buf
}

// Helper to get leading whitespace
func leadingWhitespace(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}
