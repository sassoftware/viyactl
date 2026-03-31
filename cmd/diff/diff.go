// Copyright © 2026, SAS Institute Inc., Cary, NC, USA.  All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0
// Package diff is the spf13/Cobra handler for the diff command
package diff

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/gonvenience/ytbx"
	"github.com/homeport/dyff/pkg/dyff"
	"github.com/sassoftware/viyactl/cmd"
	configtypes "github.com/sassoftware/viyactl/cmd/configTypes"
	"github.com/sassoftware/viyactl/cmd/environment"
	"golang.org/x/sync/errgroup"

	"github.com/spf13/cobra"
	"go.uber.org/multierr"
	"go.uber.org/zap"
)

// diffCmd represents the diff command
var diffCmd = &cobra.Command{
	Use:   "diff left right",
	Short: "Create a human-readable diff of two environments",
	Long: `Create a human-readable diff of two environments

Takes two environments 'left' and 'right' and produces a human-readable diff.
'left' and 'right' are one of: a path to a directory containing a configuration, a SAS endpoint, a git repository clonable with ssh.

Examples
  # Get the diff of two SAS Viya deployments at example.com and sas.com
  viyactl diff https://example.com https://sas.com

  # Get the diff of a local configuration and a SAS Viya deployment
  viyactl diff https://example.com ./site-config

  # Get the diff of two local configurations
  viyactl diff ./site-config ./viya-config

 # Get the diff of a local configuration and a git configuration
  viyactl diff git@gitlab.sas.com:example/example.git ./site-config --pem-file ~/.ssh/<private key file> --git-path <path to config in repo>
`,
	Args: cobra.ExactArgs(2),
	RunE: func(dcmd *cobra.Command, args []string) error {
		left := args[0]
		right := args[1]

		var err error

		totalDiffs, err := diff(dcmd, left, right)
		_, _ = fmt.Fprintf(dcmd.OutOrStdout(), "%d Diffs\n", totalDiffs)
		return err
	},
}

func diff(dcmd *cobra.Command, left, right string) (int, error) {
	leftEnv := configtypes.NewEnv()
	rightEnv := configtypes.NewEnv()

	var eg errgroup.Group
	var leftToken, rightToken environment.Token

	eg.Go(func() error {
		var err error
		leftToken, err = environment.Authenticate(context.Background(), left)
		if err != nil {
			return fmt.Errorf("unable to authenticate: %s", err.Error())
		}
		return nil
	})

	eg.Go(func() error {
		var err error
		rightToken, err = environment.Authenticate(context.Background(), right)
		if err != nil {
			return fmt.Errorf("unable to authenticate: %s", err.Error())
		}
		return nil
	})

	if err := eg.Wait(); err != nil {
		return 0, err
	}

	var wg sync.WaitGroup
	var err error
	for _, ct := range leftEnv {
		wg.Add(1)
		go func() {
			if multierr.AppendInto(&err, ct.Read(leftToken, left)) {
				zap.S().Warn(err)
			}
			wg.Done()
		}()
	}

	for _, ct := range rightEnv {
		wg.Add(1)
		go func() {
			if multierr.AppendInto(&err, ct.Read(rightToken, right)) {
				zap.S().Warn(err)
			}
			wg.Done()
		}()
	}
	wg.Wait()

	var errors error
	var totalDiffs int
	for i := range configtypes.SupportedTypes {
		leftYAML, err := leftEnv[i].YAML()
		errors = multierr.Append(errors, err)
		rightYAML, err := rightEnv[i].YAML()
		errors = multierr.Append(errors, err)

		leftNodes, err := ytbx.LoadYAMLDocuments(leftYAML)
		errors = multierr.Append(errors, err)
		rightNodes, err := ytbx.LoadYAMLDocuments(rightYAML)
		errors = multierr.Append(errors, err)

		leftInputFile := ytbx.InputFile{
			Location:  "in-memory",
			Note:      "left",
			Documents: leftNodes,
			Names:     []string{"Left"},
		}
		rightInputFile := ytbx.InputFile{
			Location:  "in-memory",
			Note:      "right",
			Documents: rightNodes,
			Names:     []string{"Right"},
		}

		report, err := dyff.CompareInputFiles(leftInputFile, rightInputFile, dyff.IgnoreOrderChanges(true), dyff.IgnoreWhitespaceChanges(true))
		errors = multierr.Append(errors, err)
		if err != nil {
			zap.S().Warnf("unable to compare", "configType", configtypes.SupportedTypes[i].Name())
			continue
		}

		totalDiffs += len(report.Diffs)

		if len(report.Diffs) > 0 {
			reportWriter := &dyff.HumanReport{
				Report:            report,
				DoNotInspectCerts: false,
				NoTableStyle:      false, // Side by side if false
				OmitHeader:        true,
				UseGoPatchPaths:   false,
			}
			err = reportWriter.WriteReport(dcmd.OutOrStdout())
			errors = multierr.Append(errors, err)
		}
	}

	return totalDiffs, errors
}

func init() {
	cmd.RootCmd.AddCommand(diffCmd)

	for _, configType := range configtypes.SupportedTypes {
		configTypeGenerateReportCommand := &cobra.Command{
			Use:   configType.Name(),
			Short: fmt.Sprintf("Generate the diff of %s of 2 environments", configType.Name()),
			Args:  cobra.ExactArgs(2),
			RunE: func(_ *cobra.Command, args []string) error {
				configtypes.SupportedTypes = slices.DeleteFunc(configtypes.SupportedTypes, func(s configtypes.ConfigType) bool { return s.Name() != configType.Name() })
				return diffCmd.RunE(diffCmd, args)
			},
		}
		diffCmd.AddCommand(configTypeGenerateReportCommand)
	}
}
