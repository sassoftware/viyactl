// Copyright © 2026, SAS Institute Inc., Cary, NC, USA.  All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0
package read

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func FuzzRunArgs(f *testing.F) {
	f.Add("a b c")
	f.Add("--flag value")

	f.Fuzz(func(_ *testing.T, s string) {
		args := strings.Fields(s)
		if len(args) > 0 {

			cmd := &cobra.Command{}
			_ = run(cmd, args)
		}
	})
}
