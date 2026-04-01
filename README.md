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

## Additional Resources
Viyactl code has been written with the intention of using [pkgsite](https://pkg.go.dev/golang.org/x/pkgsite/cmd/pkgsite) for code docs.
