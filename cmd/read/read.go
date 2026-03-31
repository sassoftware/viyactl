// Copyright © 2026, SAS Institute Inc., Cary, NC, USA.  All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0
// Package read is the spf13/Cobra handler for the read command
package read

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	configtypes "github.com/sassoftware/viyactl/cmd/configTypes"
	"github.com/sassoftware/viyactl/cmd/environment"

	"github.com/sassoftware/viyactl/cmd"
	"github.com/spf13/cobra"
	"go.uber.org/multierr"
	"go.uber.org/zap"
)

// readCmd represents the read command
var readCmd = &cobra.Command{
	Use:   "read environment",
	Short: "Get configuration from a SAS Viya deployment",
	Long: `Get configuration from a SAS Viya deployment.

Takes one url and gets all SAS Configuration Manager supported configuration from it.
Examples:
  # Get all config from example.com and print it
  viyactl read https://example.com

  # Get all config from example.com and save it into site-config
  viyactl read https://example.com --output-directory ./site-config

  # Get all rules with a principalType of user
  viyactl read https://example.com --disable caslibs --disable configs --disable groups --disable folders --rule-condition "eq(principalType,'user')"

  # Use a filter file
  viyactl read https://example.com --filter-file filter.yaml
`,
	Args: cobra.ExactArgs(1),
	RunE: run,
}

// Read calls the Read method of all Supported types, and returns all errors
func Read(sasEndpoint string, token environment.Token) error {
	var err error
	for _, ct := range configtypes.SupportedTypes {
		if multierr.AppendInto(&err, ct.Read(token, sasEndpoint)) {
			zap.S().Warn(err)
		}
	}
	return err
}

func run(rcmd *cobra.Command, args []string) error {
	var err error

	_, err = url.Parse(args[0])
	if err != nil {
		return err
	}

	if !strings.HasPrefix(args[0], "https://") {
		return fmt.Errorf(`%q was not a valid URL, must start with "https://"`, args[0])
	}

	outDir, err := rcmd.Flags().GetString("output-directory")
	if err != nil {
		return fmt.Errorf("unable to get output-directory argument")
	}

	zap.S().Infow(cmd.Symbols["notify"]+"authenticating with Viya environment", "url", args[0])
	t, err := environment.Authenticate(context.Background(), args[0])
	if err != nil {
		return fmt.Errorf("unable to authenticate: %s", err.Error())
	}

	zap.S().Infow(cmd.Symbols["notify"]+"reading from Viya environment", "url", args[0])
	err = Read(args[0], t)
	if err != nil {
		return fmt.Errorf("unable to read: %s", err.Error())
	}

	if outDir != "" {
		err := os.MkdirAll(outDir, 0o750)
		if err != nil {
			return err
		}
	}

	var errors error
	for _, ct := range configtypes.SupportedTypes {
		s, err := ct.YAML()
		if err != nil {
			errors = multierr.Append(errors, err)
			continue
		}
		if outDir != "" {
			err := os.WriteFile(filepath.Join(outDir, ct.FileName()), s, 0o660)
			if err != nil {
				errors = multierr.Append(errors, err)
				continue
			}
		} else {
			_, _ = fmt.Fprintln(rcmd.OutOrStdout(), string(s))
		}
	}

	if outDir != "" {
		_, _ = fmt.Fprintf(rcmd.OutOrStdout(), " Configuration written to %s\n", outDir)
	}
	return errors
}

func init() {
	cmd.RootCmd.AddCommand(readCmd)
	readCmd.PersistentFlags().StringP("output-directory", "o", "", "where output YAML is written to")

	for _, configType := range configtypes.SupportedTypes {
		configTypeReadCommand := &cobra.Command{
			Use:   configType.Name(),
			Short: fmt.Sprintf("Get %s from a SAS Viya deployment", configType.Name()),
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				configtypes.SupportedTypes = slices.DeleteFunc(configtypes.SupportedTypes, func(s configtypes.ConfigType) bool { return s.Name() != configType.Name() })
				return readCmd.RunE(readCmd, args)
			},
		}
		readCmd.AddCommand(configTypeReadCommand)
	}
}
