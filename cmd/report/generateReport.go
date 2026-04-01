// Copyright © 2026, SAS Institute Inc., Cary, NC, USA.  All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0
// Package report allows a user to generate a 'pretty' HTML report detailing differences between an arbitrary number of environments
package report

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"slices"

	"golang.org/x/sync/errgroup"

	"github.com/gonvenience/neat"
	"github.com/gonvenience/ytbx"
	"github.com/homeport/dyff/pkg/dyff"
	"github.com/sassoftware/viyactl/cmd"
	configtypes "github.com/sassoftware/viyactl/cmd/configTypes"
	"github.com/sassoftware/viyactl/cmd/environment"
	"github.com/spf13/cobra"
	"go.uber.org/multierr"
	"go.uber.org/zap"
)

// generateReportCmd represents the generateReport command
var generateReportCmd = &cobra.Command{
	Use:   "generate-report [environments]",
	Short: "Generate a html report of the diff of multiple environments",
	Long: `Generate a html report of the diff of multiple environments

The first environment given is treated as a 'Base' which all other environments are compared to.
The report is generated as 'viyactl-report-HH-MM_DD-MM-YYYY.html'.

Examples
  # Get a report of the full diff between two SAS Viya deployments at example.com and sas.com
  viyactl generate-report https://example.com https://sas.com

  # Get a report of the diff of groups between two SAS Viya deployments at example.com and sas.com
  viyactl generate-report groups https://example.com https://sas.com

  # Get a filtered report of the full diff between two SAS Viya deployments at example.com and sas.com
  viyactl generate-report https://example.com https://sas.com --filter-file filter.yaml

  # Compare a local 'golden config' to multiple environments
  viyactl generate-report ./golden https://example.com https://sas.com
`,
	Args: cobra.MinimumNArgs(2),
	RunE: run,
}

func run(rcmd *cobra.Command, args []string) error {
	var err error

	ctx := context.Background()

	wg, _ := errgroup.WithContext(ctx)

	envs := make([][]configtypes.ConfigType, len(args))
	for i, host := range args {
		wg.Go(func() error {
			env := configtypes.NewEnv()
			token, err := environment.Authenticate(context.Background(), host)
			if err != nil {
				return fmt.Errorf("unable to authenticate: %s", err.Error())
			}

			for _, ct := range env {
				if multierr.AppendInto(&err, ct.Read(token, host)) {
					zap.S().Warn(err)
				}
			}
			envs[i] = env
			return err
		})
	}

	if err := wg.Wait(); len(multierr.Errors(err)) > 0 {
		return err
	}

	input := map[string]pair{}

	for i, baseConfigType := range envs[0] {
		var diffs []map[string][]Difference
		for _, env := range envs[1:] {
			d, err := makeDiffs(baseConfigType, env[i])
			if err != nil {
				return err
			}
			diffs = append(diffs, d)
		}
		input[baseConfigType.Name()] = pair{diffs, baseConfigType}
	}

	filename := fmt.Sprintf("./viyactl-report-%s.html", cmd.GetDateString())
	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("unable to create %s: %s", filename, err.Error())
	}
	defer func() {
		err := f.Close()
		if err != nil {
			_, _ = fmt.Fprintf(rcmd.OutOrStdout(), "Unable to close file %q\n", args[0])
		}
	}()

	htmlReport := HTMLReportDetails{
		Base:         envs[0],
		BaseName:     args[0],
		Environments: args[1:],
	}

	err = Page(htmlReport, input).Render(context.Background(), f)
	if err != nil {
		return fmt.Errorf("unable to render report: %s", err.Error())
	}
	_, _ = fmt.Fprintln(rcmd.OutOrStdout(), cmd.Symbols["success"], " Report created at", filename)

	return nil
}

func makeDiffs(base, cmp configtypes.ConfigType) (map[string][]Difference, error) {
	var errors error
	baseYaml, err := base.YAML()
	errors = multierr.Append(errors, err)
	cmpYaml, err := cmp.YAML()
	errors = multierr.Append(errors, err)
	cmpBuf := bytes.NewBuffer(cmpYaml)
	baseBuf := bytes.NewBuffer(baseYaml)

	if err != nil {
		return nil, err
	}

	baseYaml = environment.PatchBuffer(baseBuf).Bytes()
	cmpYaml = environment.PatchBuffer(cmpBuf).Bytes()

	leftNodes, err := ytbx.LoadYAMLDocuments(baseYaml)
	errors = multierr.Append(errors, err)
	rightNodes, err := ytbx.LoadYAMLDocuments(cmpYaml)
	errors = multierr.Append(errors, err)

	if errors != nil {
		return nil, err
	}

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
	if err != nil {
		zap.S().Warnf("unable to compare", "configType", base.Name())
		return nil, err
	}

	reportWriter := &dyff.HumanReport{
		Report: report,
	}

	diffs := make(map[string][]Difference)
	for _, diff := range reportWriter.Diffs {
		for _, detail := range diff.Details {
			from := ""
			to := ""
			if detail.To != nil {
				// If string then value will be nonempty, avoid ToYAMLString here as it will add quotes to string
				to = detail.To.Value
				// If value was empty, then the data is not a string
				if to == "" {
					to, _ = neat.ToYAMLString(detail.To)
				}
			}
			if detail.From != nil {
				from = detail.From.Value
				if from == "" {
					from, _ = neat.ToYAMLString(detail.From)
				}
			}
			diffs[diff.Path.String()] = append(diffs[diff.Path.String()], Difference{To: to, From: from})
		}
	}
	return diffs, nil
}

// Difference represents one difference between two YAML files
type Difference struct {
	Type string
	From string
	To   string
}

// HTMLReportDetails is all data needed to construct a HTML report
type HTMLReportDetails struct {
	Base         []configtypes.ConfigType
	BaseName     string
	Environments []string
}

type pair struct {
	diffs []map[string][]Difference
	base  any
}

func init() {
	cmd.RootCmd.AddCommand(generateReportCmd)

	for _, configType := range configtypes.SupportedTypes {
		configTypeGenerateReportCommand := &cobra.Command{
			Use:   configType.Name(),
			Short: fmt.Sprintf("Generate a html report of the diff of %s of multiple environments", configType.Name()),
			Args:  cobra.MinimumNArgs(2),
			RunE: func(_ *cobra.Command, args []string) error {
				configtypes.SupportedTypes = slices.DeleteFunc(configtypes.SupportedTypes, func(s configtypes.ConfigType) bool { return s.Name() != configType.Name() })
				return generateReportCmd.RunE(generateReportCmd, args)
			},
		}
		generateReportCmd.AddCommand(configTypeGenerateReportCommand)
	}
}
