package cmd

import (
	"os"
	"regexp"
	"testing"
)

func Test_TransformFQDN_Unit(t *testing.T) {
	fqdnTests := []struct {
		input string
		want  string
	}{
		{"https://example.com", "EXAMPLE_COM"},
		{"https://test.example.com", "TEST_EXAMPLE_COM"},
		{"https://example.com/env", "EXAMPLE_COM_ENV"},
		{"https://example.com/env-a", "EXAMPLE_COM_ENV_A"},
		{"https://example.com   /env", "EXAMPLE_COM_ENV"},
		{"https://EXAMPLE", "EXAMPLE"},
	}
	for _, tt := range fqdnTests {
		got := transformFQDN(tt.input)
		if got != tt.want {
			t.Errorf("got %s want %s", got, tt.want)
		}
	}
}

func Test_GetCredentials_Unit(t *testing.T) {
	fqdnTests := []struct {
		endpoint string
		set      map[string]string
		want     AuthInfo
	}{
		{
			"https://example.com",
			map[string]string{"USERNAME": "Name", "PASSWORD": "password"},
			AuthInfo{Username: "Name", Password: "password"},
		},
	}
	for _, tt := range fqdnTests {
		for k, v := range tt.set {
			err := os.Setenv(k, v)
			if err != nil {
				t.Errorf("unable to set environment variable: %q", k)
			}
		}
		got, err := GetCredentials(tt.endpoint)
		if err != nil {
			t.Errorf("unable to authenticate: %q", err)
		}
		if got != tt.want {
			t.Errorf("got %s want %s", got, tt.want)
		}
	}
}

func Test_GetDateString_Unit(t *testing.T) {
	want := regexp.MustCompile(`\d{2}-\w{3}-\d{2}_\d{2}-\d{2}-\w{3}`)
	got := GetDateString()

	if !want.Match([]byte(got)) {
		t.Errorf("Date string did not match expected format: got %q", got)
	}
}
