// Copyright © 2026, SAS Institute Inc., Cary, NC, USA.  All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0
package diff

import (
	"context"
	"io"
	"log"
	"os"
	"testing"

	"github.com/sassoftware/viyactl/cmd/environment"
)

func Test_NoDiffSame_Integration(t *testing.T) {
	sasEndpoint := os.Getenv("SAS_ENDPOINT")

	if sasEndpoint == "" {
		t.Skip("set [SAS_ENDPOINT] to run SAS Viya integration tests")
		return
	}

	diffCmd.SetOut(io.Discard)
	if got, err := diff(diffCmd, sasEndpoint, sasEndpoint); got != 0 || err != nil {
		t.Errorf("Expected 0 differences, got %d, err %q", got, err)
	}
}

var AuthToken environment.Token

func TestMain(m *testing.M) {
	setup()
	ret := m.Run()
	if ret == 0 {
		teardown()
	}
	os.Exit(ret)
}

func setup() {
	sasEndpoint := os.Getenv("SAS_ENDPOINT")

	if sasEndpoint == "" {
		log.Println("set [SAS_ENDPOINT] to run SAS Viya integration tests")
	} else {
		var err error
		AuthToken, err = environment.Authenticate(context.Background(), sasEndpoint)
		if err != nil {
			log.Fatalf("setup(Authenticate), unable to authenticate: %s", err.Error())
		}
	}
}

func teardown() {
}
