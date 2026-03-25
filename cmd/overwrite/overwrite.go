// Package overwrite manages writing configurations to SAS Viya deployments
package overwrite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/sassoftware/viyactl/cmd"
	configtypes "github.com/sassoftware/viyactl/cmd/configTypes"
	"github.com/sassoftware/viyactl/cmd/environment"
	"github.com/spf13/cobra"
	"go.uber.org/multierr"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// overwriteCmd represents the overwrite command
var overwriteCmd = &cobra.Command{
	Use:   "overwrite base [environments]",
	Short: "Write a configuration to a SAS Viya environment",
	Long: `Write a configuration to a SAS Viya environment

Takes two or more environments and makes all subsequent environments have the same state as the first.
The first env is either the path to a directory containing a configuration or a SAS endpoint.
Subsequent environments must be a SAS endpoints.

If the output flag is specified, will save pre write values of subsequent environments to directories corresponding to their hosts under the current directory (E.g. https://a.sas.com -> a-sas-com).

Examples
  # Write a 'golden config' to a SAS Viya deployment
  viyactl overwrite ./golden https://example.com

  # Write one environment to another
  viyactl overwrite https://sas.com https://example.com

  # Write a config to multiple environments
  viyactl overwrite ./golden https://sas.com https://example.com
`,
	Args: cobra.MinimumNArgs(2),
	RunE: run,
}

func run(overwriteCmd *cobra.Command, args []string) error {
	left := args[0]

	leftEnv := configtypes.NewEnv()

	leftToken, err := environment.Authenticate(context.Background(), left)
	if err != nil {
		return fmt.Errorf("unable to authenticate: %s", err.Error())
	}

	var wg errgroup.Group
	for _, ct := range leftEnv {
		wg.Go(func() error {
			multierr.AppendInto(&err, ct.Read(leftToken, left))
			return err
		})
	}

	saveEnvironmentBeforeWrite, err := overwriteCmd.Flags().GetBool("output")
	if err != nil {
		return fmt.Errorf("unable to get output-directory argument")
	}

	viyaEnvs := make([][]configtypes.ConfigType, len(args)-1)
	tokens := make([]environment.Token, len(args)-1)
	for i, host := range args[1:] {
		wg.Go(func() error {
			token, err := environment.Authenticate(context.Background(), host)
			if err != nil {
				return fmt.Errorf("unable to authenticate: %s", err.Error())
			}
			tokens[i] = token

			viyaEnvs[i] = configtypes.NewEnv()
			for _, ct := range viyaEnvs[i] {
				if multierr.AppendInto(&err, ct.Read(token, host)) {
					zap.S().Warn(err)
				}
			}

			if saveEnvironmentBeforeWrite {
				for _, ct := range viyaEnvs[i] {
					s, err := ct.YAML()
					if err != nil {
						return err
					}

					if outDir := transformFQDN(host); outDir != "" {
						outDir = fmt.Sprintf("%s-%s", outDir, cmd.GetDateString())
						err = os.Mkdir(outDir, 0o750)
						if err != nil && !os.IsExist(err) {
							return err
						}
						err = os.WriteFile(filepath.Join(outDir, ct.FileName()), s, 0o660)
						if err != nil {
							return err
						}
					}

				}
			}
			return nil
		})
	}

	if err := wg.Wait(); err != nil {
		return err
	}

	for i, host := range args[1:] {
		wg.Go(func() error {
			var errors error
			for j, ct := range viyaEnvs[i] {
				multierr.AppendInto(&errors, ct.Write(tokens[i], host, leftEnv[j]))
			}
			return errors
		})
	}

	err = wg.Wait()
	return err
}

func transformFQDN(sasEndpoint string) string {
	_, fqdn, _ := strings.Cut(sasEndpoint, "://")

	var builder strings.Builder
	lastCharHyphen := false

	for _, char := range fqdn {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			builder.WriteRune(char)
			lastCharHyphen = false
		} else {
			if !lastCharHyphen {
				builder.WriteRune('-')
				lastCharHyphen = true
			}
		}
	}

	return strings.ToLower(builder.String())
}

func init() {
	cmd.RootCmd.AddCommand(overwriteCmd)
	overwriteCmd.PersistentFlags().String("host-output-directory", "", "where host environment YAML gets written")
	overwriteCmd.PersistentFlags().Bool("output", false, "whether to output pre-overwrite YAML")

	for _, configType := range configtypes.SupportedTypes {
		configTypeOverwriteCommand := &cobra.Command{
			Use:   configType.Name(),
			Short: fmt.Sprintf("Write %s to a SAS Viya environment", configType.Name()),
			Args:  cobra.MinimumNArgs(2),
			RunE: func(_ *cobra.Command, args []string) error {
				configtypes.SupportedTypes = slices.DeleteFunc(configtypes.SupportedTypes, func(s configtypes.ConfigType) bool { return s.Name() != configType.Name() })
				return overwriteCmd.RunE(overwriteCmd, args)
			},
		}
		overwriteCmd.AddCommand(configTypeOverwriteCommand)
	}
}
