// Copyright © 2026, SAS Institute Inc., Cary, NC, USA.  All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0
// Package cmd handles performing all actions of the CLI script
package cmd

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	// RedactedConfigs is a list of keys that should be redacted in logs and hashed in readers
	RedactedConfigs = []string{"password", "serviceProviderKey"}
	// Symbols are the emojis used to indicate status, values may be replaced (e.g. when using --ASCII)
	Symbols = map[string]string{
		"notify":  `🔔 `,
		"fail":    "✖️ ",
		"success": "✔ ",
	}
)

func initLogger() error {
	var logger *zap.Logger
	var err error
	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(LogLevel)
	cfg.DisableStacktrace = true
	if Verbose {
		cfg = zap.NewDevelopmentConfig()
		cfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}
	logger, err = cfg.Build()
	if err != nil {
		return fmt.Errorf("unable to create logger: %s", err.Error())
	}
	zap.ReplaceGlobals(logger)
	return nil
}

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:          "viyactl",
	SilenceUsage: true,
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		if ASCII {
			Symbols["fail"] = "️[fail] "
			Symbols["success"] = "[success] "
			Symbols["notify"] = "[notify] "
		}

		err := initLogger()
		if err != nil {
			return err
		}

		Client.Timeout = Timeout

		if AuthInfoPath != "" {
			// https://go.documentation.sas.com/doc/en/pgmsascdc/v_067/authinfo/titlepage.htm
			// Accept SAS acceptable aliases for host, port and user
			authCodeR := regexp.MustCompile(
				`(?:(?:host|machine)\s+(?P<host>.*)|(?P<host>default))\s+` +
					`client-id\s+(?P<clientID>.*)\s+` +
					`client-secret\s+(?P<clientSecret>.*)`,
			)

			usernamePasswordR := regexp.MustCompile(
				`(?:(?:host|machine)\s+(?P<host>.*)|(?P<host>default))\s+` +
					`(?:user|login)\s+(?P<username>.*)\s+` +
					`password\s+(?P<password>.*)`,
			)

			file, err := os.Open(AuthInfoPath)
			if err != nil {
				return fmt.Errorf("error opening file: %s", err.Error())
			}
			defer func() {
				err := file.Close()
				if err != nil {
					_, _ = fmt.Printf("Unable to close file %q\n", AuthInfoPath)
				}
			}()

			Auth = make(map[string]AuthInfo)

			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := scanner.Text()
				switch {
				case authCodeR.MatchString(line):

					matches := authCodeR.FindStringSubmatch(line)
					if v, found := Auth[matches[authCodeR.SubexpIndex("host")]]; found {

						v.ClientID = matches[authCodeR.SubexpIndex("clientID")]
						v.ClientSecret = matches[authCodeR.SubexpIndex("clientSecret")]
						Auth[matches[authCodeR.SubexpIndex("host")]] = v
					} else {
						Auth[matches[authCodeR.SubexpIndex("host")]] = AuthInfo{
							ClientID:     matches[authCodeR.SubexpIndex("clientID")],
							ClientSecret: matches[authCodeR.SubexpIndex("clientSecret")],
						}
					}

				case usernamePasswordR.MatchString(line):
					matches := usernamePasswordR.FindStringSubmatch(line)

					if v, found := Auth[matches[authCodeR.SubexpIndex("host")]]; found {
						v.Username = matches[usernamePasswordR.SubexpIndex("username")]
						v.Password = matches[usernamePasswordR.SubexpIndex("password")]
						Auth[matches[authCodeR.SubexpIndex("host")]] = v
					} else {
						Auth[matches[usernamePasswordR.SubexpIndex("host")]] = AuthInfo{
							Username: matches[usernamePasswordR.SubexpIndex("username")],
							Password: matches[usernamePasswordR.SubexpIndex("password")],
						}
					}
				default:
					zap.S().Warnw("Could not recognise authinfo entry", "line", line)
				}
			}

			if err := scanner.Err(); err != nil {
				return fmt.Errorf("error reading %s: %s", file.Name(), err.Error())
			}
		}
		return nil
	},
	Short: "viyactl allows for interacting with the configuration of SAS Viya deployments declaratively",
	Long: `viyactl allows for interacting with the configuration of SAS Viya deployments declaratively.

Latest build is located at https://github.com/sassoftware/viyactl/releases
Built at: ` + buildTime + `

When an authinfo file is provided using the --auth-info flag it will take precedence over environment variables, with client-id and client-secret taking precedence over username and password.

Given a Viya deployment at "https://sas.example.com", set SAS_EXAMPLE_COM_USERNAME" and "SAS_EXAMPLE_COM_PASSWORD" or SAS_EXAMPLE_COM_CLIENT_ID" and "SAS_EXAMPLE_COM_CLIENT_SECRET".
If these environment variables are not set viyactl will use "USERNAME" and "PASSWORD" or CLIENT_ID and CLIENT_SECRET for authentication`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := RootCmd.Execute()
	if err != nil {
		_ = zap.S().Sync()
		os.Exit(1)
	}
	_ = zap.S().Sync()
}

//go:embed datetime.txt
var buildTime string

var (
	// LogLevel determines the level the logs are at
	LogLevel zapcore.Level = zap.WarnLevel
	// Timeout represents a timeout that can be parsed with time.ParseDuration
	Timeout time.Duration
	// AuthInfoPath is a file to a .authinfo file with contents defined by //TODO
	AuthInfoPath string
	// Disable is a list of disabled readers (caslibs, configs, folders, groups, rules)
	Disable []string
	// ASCII makes only ASCII chars used (disables unicode)
	ASCII bool
	// Verbose indicates if the program should be in verbose mode
	Verbose bool
	// RuleFilters is a list of filters for retrieving rules
	RuleFilters []string
	// FolderFilters is a list of filters for retrieving folders
	FolderFilters []string
	// CaslibFilters is a list of filters for retrieving caslibs
	CaslibFilters []string
	// GroupFilters is a list of filters for retrieving groups
	GroupFilters []string
	// ConfigName restricts configs to objects with the given definitionName
	ConfigName string
	// ConfigService restricts configs to objects with the given service
	ConfigService string
	// FilterFile is the path to a yaml file containing documents for caslibs, folders, groups and rules. configs are not supported with this file (instead use '--config-name' and '--config-service')
	FilterFile string
	// ConfigFilter is a best effort attempt at filtering configs, unlike other filters does not reduce http requests
	ConfigFilter []byte
	// Client is a http client used throughout in accordance with https://pkg.go.dev/net/http#pkg-overview ('Clients... should only be created once and re-used')
	Client http.Client

	// Auth is a map of hosts to their authInfo
	Auth map[string]AuthInfo
)

// AuthInfo is the credentials that should get associated with a host by Auth
type AuthInfo struct {
	Username     string
	Password     string
	ClientID     string
	ClientSecret string
}

func init() {
	var flags []*pflag.FlagSet

	fs := flag.NewFlagSet("pure-go-flags", flag.ContinueOnError)
	fs.Var(&LogLevel, "log-level", "log level (debug, info, warn, error, dpanic, panic, fatal)")
	if f := fs.Lookup("log-level"); f != nil {
		RootCmd.PersistentFlags().AddGoFlag(f)
	}

	others := pflag.NewFlagSet("Flags", pflag.ExitOnError)
	others.DurationVar(&Timeout, "timeout", 10*time.Second, "how long before HTTP operations time out (parsed with go time.ParseDuration)")
	others.StringVar(&AuthInfoPath, "auth-info", "", "path to .authinfo file, https://go.documentation.sas.com/doc/en/pgmsascdc/v_067/authinfo/titlepage.htm, Viyactl also allows replacing username and password with client-id and client-secret to use UAA")
	others.StringArrayVar(&Disable, "disable", []string{}, "list of configuration types that will not be read/written (caslibs, configs, folders, groups, rules)")
	others.BoolVarP(&Verbose, "verbose", "v", false, "enable verbose mode - use zap#development")
	others.BoolVar(&ASCII, "ascii", false, "enable ascii only mode")
	flags = append(flags, others)

	filters := pflag.NewFlagSet("Filter Flags", pflag.ExitOnError)
	filters.StringArrayVar(&CaslibFilters, "caslib-filter", []string{}, "filters for retrieving caslibs, https://developer.sas.com/rest-apis/casManagement/getCaslibs#query-parameters")
	filters.StringArrayVar(&RuleFilters, "rule-filter", []string{}, "filters for retrieving rules, https://developer.sas.com/rest-apis/authorization/getRules#query-parameters")
	filters.StringArrayVar(&FolderFilters, "folder-filter", []string{}, "filters for retrieving folders, https://developer.sas.com/rest-apis/folders/getFolders#query-parameters")
	filters.StringArrayVar(&GroupFilters, "group-filter", []string{}, "filters for retrieving groups, https://developer.sas.com/rest-apis/identities/getGroups#query-parameters")
	filters.StringVar(&ConfigName, "config-name", "", "restrict configs to objects with the given definitionName, https://developer.sas.com/rest-apis/configuration/getConfigurations#query-parameters")
	filters.StringVar(&ConfigService, "config-service", "", "restrict configs to objects with the given service, https://developer.sas.com/rest-apis/configuration/getConfigurations#query-parameters")
	filters.StringVar(&FilterFile, "filter-file", "", "path to filter file, this is a YAML file containing documents for caslibs, folders, groups and rules. configs are not supported in filter files (but '--config-name' and '--config-service' can still be used with filter files)")

	filters.Usage = func() {
		fmt.Println(`Filters use the query parameter documented at https://developer.sas.com/docs/rest-apis/getting-started/usage-notes#filtering
Filters are used as part of a query, e.g. : 'https://sas.endpoint/api?filter=and(condition_1,condition_2,...condition_n)'`)
	}
	flags = append(flags, filters)

	for _, f := range flags {
		RootCmd.PersistentFlags().AddFlagSet(f)
	}
	RootCmd.MarkFlagsMutuallyExclusive("filter-file", "caslib-filter")
	RootCmd.MarkFlagsMutuallyExclusive("filter-file", "rule-filter")
	RootCmd.MarkFlagsMutuallyExclusive("filter-file", "folder-filter")
	RootCmd.MarkFlagsMutuallyExclusive("filter-file", "group-filter")
}

// ExitWithHTTPError provides a unified way of formatting an error when a HTTP request does not return the expected status code
func ExitWithHTTPError(res *http.Response, expected, apiReference string) error {
	b, err := io.ReadAll(res.Body)

	var errStr string
	errStr += fmt.Sprintf("expected %s got %d, API reference: %q", expected, res.StatusCode, apiReference)
	if err != nil {
		return errors.New(errStr + "\nunable to read from response body")
	}
	j := struct {
		Message     string   `json:"message"`
		Details     []string `json:"details"`
		Remediation []string `json:"remediation"`
		Errors      []any    `json:"errors"`
	}{}

	err = json.Unmarshal(b, &j)
	if err != nil {
		errStr += "unable to extract details from response body"
	}

	if j.Message != "" {
		errStr += "\nmessage: " + j.Message
	}
	if len(j.Details) > 0 {
		errStr += fmt.Sprintf("\ndetails: %q", j.Details)
	}
	if len(j.Remediation) > 0 {
		errStr += fmt.Sprintf("\nremediation: %q", j.Remediation)
	}
	if len(j.Errors) > 0 {
		errStr += fmt.Sprintf("\nerrors: %q", j.Errors)
	}
	errStr += "\n"
	return errors.New(errStr)
}

func transformFQDN(sasEndpoint string) string {
	_, fqdn, _ := strings.Cut(sasEndpoint, "://")

	var builder strings.Builder
	lastCharUnderscore := false

	for _, char := range fqdn {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			builder.WriteRune(char)
			lastCharUnderscore = false
		} else {
			if !lastCharUnderscore {
				builder.WriteRune('_')
				lastCharUnderscore = true
			}
		}
	}

	return strings.ToUpper(builder.String())
}

// AuthenticationType is an enum which determines which authentication method is used
type AuthenticationType uint

const (
	// UsernamePassword represents that a username and password are used for authentication
	UsernamePassword = iota + 1
	// AuthCode represents that a clientID and clientSecret are used for authentication
	AuthCode
)

// GetCredentials gets the USERNAME and PASSWORD for a sasEndpoint, if these are not defined it uses global USERNAME and PASSWORD as a fallback
func GetCredentials(sasEndpoint string) (AuthInfo, error) {
	fqdn := transformFQDN(sasEndpoint)

	username := os.Getenv(fqdn + "_USERNAME")
	password := os.Getenv(fqdn + "_PASSWORD")
	clientID := os.Getenv(fqdn + "_CLIENT_ID")
	clientSecret := os.Getenv(fqdn + "_CLIENT_SECRET")

	if username == "" {
		username = os.Getenv("USERNAME")
	}
	if password == "" {
		password = os.Getenv("PASSWORD")
	}
	if clientID == "" {
		clientID = os.Getenv("CLIENT_ID")
	}
	if clientSecret == "" {
		clientSecret = os.Getenv("CLIENT_SECRET")
	}

	if (username == "" || password == "") && (clientID == "" || clientSecret == "") {
		return AuthInfo{}, fmt.Errorf(`could not retrieve USERNAME and PASSWORD or CLIENT_ID and CLIENT_SECRET for %q\nPlease set either environment variables %q and %q, or "USERNAME" and "PASSWORD" or "CLIENT_ID" and "CLIENT_SECRET" or provide an authinfo file`, sasEndpoint, fqdn+"_USERNAME", fqdn+"_PASSWORD")
	}

	return AuthInfo{
		Username:     username,
		Password:     password,
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}, nil
}

// GetDateString provides a way to get current time - if the format ever needs to change only 1 line needs to change
func GetDateString() string {
	layout := "02-Jan-06_15-04-MST"
	return time.Now().Format(layout)
}
