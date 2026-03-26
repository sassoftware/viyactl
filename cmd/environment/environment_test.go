package environment

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func ExampleIsViyaDeploymentUp() {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := fmt.Fprintf(w, "login accepted")
		if err != nil {
			fmt.Println(err)
		}
	}))

	err := IsViyaDeploymentUp(s.URL)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println("SAS Viya deployment was up!")
	}
	// Output: SAS Viya deployment was up!
}

var AuthToken Token

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
		AuthToken, err = Authenticate(context.Background(), sasEndpoint)
		if err != nil {
			log.Fatalf("setup(Authenticate), unable to authenticate: %s", err.Error())
		}
	}
}

func teardown() {
}
