# Viyactl

## Overview
Viyactl allows for the declarative management of settings within SAS Viya, using a simple YAML format.

It lets users save configurations from a SAS Viya environment, and then compare and apply them to SAS Viya environments.

This allows the following patterns:
- Health checking
  - Comparing configuration between multiple environments
- Rollback
  - Saving configuration, then if future changes are suboptimal Viyactl can apply this old configuration, rolling back to a previous state
- GitOps
  - Saving configuration in a Git repository, then using CICD platforms like GitHub Actions to apply the configuration whenever it is changed


## Installation
Viyactl is distributed as a single binary with no external dependencies.
Viyactl can be installed with either of the following sets of instructions.

### Go Install
```sh
go install github.com/sassoftware/viyactl@latest
```

### Prebuilt binary
Built binaries can be downloaded from https://github.com/sassoftware/viyactl/releases
- Download viyactl build for correct OS/Architecture
- Add Viyactl to the PATH of your OS, or use the full path to the executable for all commands

### Getting Started
To get started you can follow the [quickstart guide](./website/docs/README.md)

Viyactl also has help messages for all commands, simply type `viyactl --help` or `viyactl $COMMAND --help`

#### Autocompletion
Viyactl is built with cobra which generates scripts for [shell completion](https://cobra.dev/docs/how-to-guides/shell-completion/)

## Contributing
Maintainers are accepting patches and contributions to this project.
Please read [CONTRIBUTING.md](CONTRIBUTING.md) for details about submitting contributions to this project.

## License
This project is licensed under the [Apache 2.0 License](LICENSE).

## Third-Party Dependencies
This project requires the following dependencies.

| Dependency | License |
| ---------- | ------- |
| [gopkg.in/yaml.v2](https://github.com/go-yaml/yaml) | [Apache-2.0](https://github.com/go-yaml/yaml/blob/v2.4.0/LICENSE) |
| [github.com/spf13/cobra](https://github.com/spf13/cobra) | [Apache-2.0](https://github.com/spf13/cobra/blob/v1.9.1/LICENSE.txt) |
| [github.com/gonvenience/idem](https://github.com/gonvenience/idem) | [Apache-2.0](https://github.com/gonvenience/idem/blob/v0.0.1/LICENSE) |
| [golang.org/x/crypto/blake2b](https://cs.opensource.google/go/x/crypto) | [BSD-3-Clause](https://cs.opensource.google/go/x/crypto/+/v0.45.0:LICENSE) |
| [github.com/virtuald/go-ordered-json](https://github.com/virtuald/go-ordered-json) | [BSD-3-Clause](https://github.com/virtuald/go-ordered-json/blob/b18e6e673d74/LICENSE) |
| [github.com/spf13/pflag](https://github.com/spf13/pflag) | [BSD-3-Clause](https://github.com/spf13/pflag/blob/v1.0.6/LICENSE) |
| [github.com/google/go-cmp/cmp](https://github.com/google/go-cmp) | [BSD-3-Clause](https://github.com/google/go-cmp/blob/v0.7.0/LICENSE) |
| [golang.org/x/sync](https://cs.opensource.google/go/x/sync) | [BSD-3-Clause](https://cs.opensource.google/go/x/sync/+/v0.18.0:LICENSE) |
| [golang.org/x/sys](https://cs.opensource.google/go/x/sys) | [BSD-3-Clause](https://cs.opensource.google/go/x/sys/+/v0.38.0:LICENSE) |
| [golang.org/x/term](https://cs.opensource.google/go/x/term) | [BSD-3-Clause](https://cs.opensource.google/go/x/term/+/v0.37.0:LICENSE) |
| [dario.cat/mergo](https://github.com/imdario/mergo) | [BSD-3-Clause](https://github.com/imdario/mergo/blob/v1.0.1/LICENSE) |
| [github.com/BurntSushi/toml](https://github.com/BurntSushi/toml) | [MIT](https://github.com/BurntSushi/toml/blob/v1.4.0/COPYING) |
| [github.com/a-h/templ](https://github.com/a-h/templ) | [MIT](https://github.com/a-h/templ/blob/v0.3.906/LICENSE) |
| [github.com/goccy/go-yaml](https://github.com/goccy/go-yaml) | [MIT](https://github.com/goccy/go-yaml/blob/v1.18.0/LICENSE) |
| [github.com/gonvenience/bunt](https://github.com/gonvenience/bunt) | [MIT](https://github.com/gonvenience/bunt/blob/v1.4.0/LICENSE) |
| [github.com/gonvenience/neat](https://github.com/gonvenience/neat) | [MIT](https://github.com/gonvenience/neat/blob/v1.3.15/LICENSE) |
| [github.com/gonvenience/term](https://github.com/gonvenience/term) | [MIT](https://github.com/gonvenience/term/blob/v1.0.3/LICENSE) |
| [github.com/gonvenience/text](https://github.com/gonvenience/text) | [MIT](https://github.com/gonvenience/text/blob/v1.0.8/LICENSE) |
| [github.com/gonvenience/ytbx](https://github.com/gonvenience/ytbx) | [MIT](https://github.com/gonvenience/ytbx/blob/v1.4.7/LICENSE) |
| [github.com/homeport/dyff/pkg/dyff](https://github.com/homeport/dyff) | [MIT](https://github.com/homeport/dyff/blob/v1.10.1/LICENSE) |
| [github.com/lucasb-eyer/go-colorful](https://github.com/lucasb-eyer/go-colorful) | [MIT](https://github.com/lucasb-eyer/go-colorful/blob/v1.2.0/LICENSE) |
| [github.com/mattn/go-ciede2000](https://github.com/mattn/go-ciede2000) | [MIT](https://github.com/mattn/go-ciede2000/blob/782e8c62fec3/LICENSE) |
| [github.com/mattn/go-isatty](https://github.com/mattn/go-isatty) | [MIT](https://github.com/mattn/go-isatty/blob/v0.0.20/LICENSE) |
| [github.com/mitchellh/go-ps](https://github.com/mitchellh/go-ps) | [MIT](https://github.com/mitchellh/go-ps/blob/v1.0.0/LICENSE.md) |
| [github.com/mitchellh/hashstructure](https://github.com/mitchellh/hashstructure) | [MIT](https://github.com/mitchellh/hashstructure/blob/v1.1.0/LICENSE) |
| [github.com/sergi/go-diff/diffmatchpatch](https://github.com/sergi/go-diff) | [MIT](https://github.com/sergi/go-diff/blob/v1.4.0/LICENSE) |
| [github.com/texttheater/golang-levenshtein/levenshtein](https://github.com/texttheater/golang-levenshtein) | [MIT](https://github.com/texttheater/golang-levenshtein/blob/v1.0.1/LICENSE) |
| [go.uber.org/multierr](https://github.com/uber-go/multierr) | [MIT](https://github.com/uber-go/multierr/blob/v1.11.0/LICENSE.txt) |
| [go.uber.org/zap](https://github.com/uber-go/zap) | [MIT](https://github.com/uber-go/zap/blob/v1.27.1/LICENSE) |
| [gopkg.in/yaml.v3](https://github.com/go-yaml/yaml) | [MIT](https://github.com/go-yaml/yaml/blob/v3.0.1/LICENSE) |


## Additional Resources
Viyactl code has been written with the intention of using [pkgsite](https://pkg.go.dev/golang.org/x/pkgsite/cmd/pkgsite) for code docs.
