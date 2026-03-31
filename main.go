// Copyright © 2026, SAS Institute Inc., Cary, NC, USA.  All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0
// Package main imports all commands so they are included in docs etc. and calls cmd/root.go/Execute
//
// viyactl documentation is intended to be used with pkgsite https://pkg.go.dev/golang.org/x/pkgsite/cmd/pkgsite
package main

import (
	"github.com/sassoftware/viyactl/cmd"
	_ "github.com/sassoftware/viyactl/cmd/diff"

	_ "github.com/sassoftware/viyactl/cmd/overwrite"
	_ "github.com/sassoftware/viyactl/cmd/read"
	_ "github.com/sassoftware/viyactl/cmd/report"
)

func main() {
	cmd.Execute()
}
