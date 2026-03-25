---
title: Quickstart
sidebar_position: 1
---


Viyactl handles:
- Caslibs (only cas-shared-default)
- Configs
- Folders
- Groups (only custom groups)
- Rules

## Acquiring viyactl

Application binaries can be downloaded from https://github.com/sassoftware/viyactl/releases
- Download viyactl build for correct OS/Architecture
- Add Viyactl to the PATH of your OS, or use the full path to the executable for all commands

## Using viyactl
### Getting help
Viyactl provides help messages when using the `--help` flag, e.g.
```sh
viyactl read --help
```

Which would output the following:
```
viyactl allows for interacting with the configuration of SAS Viya deployments declaratively.

Latest build is located at https://github.com/sassoftware/viyactl/releases
Built at: hh:mm-dd-mm-yyyy


When an authinfo file is provided using the --auth-info flag it will take precedence over environment variables, with client-id and client-secret taking precedence over username and password.

Given a Viya deployment at "https://sas.example.com" set SAS_EXAMPLE_COM_USERNAME" and "SAS_EXAMPLE_COM_PASSWORD" or SAS_EXAMPLE_COM_CLIENT_ID" and "SAS_EXAMPLE_COM_CLIENT_SECRET"
If these environment variables are not set viyactl will use "USERNAME" and "PASSWORD" or CLIENT_ID and CLIENT_SECRET for authentication

Usage:
  viyactl [flags]

Available Commands:
        completion       Generate the autocompletion script for the specified shell
        diff             Create a human-readable diff of two environments
        generate-report  Generate a html report of the diff of multiple environments
        help             Help about any command
        overwrite        (in-development) Write a configuration to a SAS Viya environment
        read             Get configuration from a SAS Viya deployment
        validate         Check that local configuration YAML is valid

Flags:
      --ascii                 enable ascii only mode
      --auth-info string      path to .authinfo file, https://go.documentation.sas.com/doc/en/pgmsascdc/v_067/authinfo/titlepage.htm, Viyactl also allows replacing username and password with client-id and client-secret to use UAA
      --disable stringArray   list of configuration types that will not be read/written (caslibs, configs, folders, groups, rules)
      --log-level string      level of logs (debug, info, warn) (default "warn")
      --timeout duration      how long before HTTP operations time out (parsed with go time.ParseDuration) (default 10s)
      --verbose               enable verbose mode


Filter Flags:
Filters use the query parameter documented at https://developer.sas.com/docs/rest-apis/getting-started/usage-notes#filtering
Filters are used as part of a query, e.g. : 'https://sas.endpoint/api?filter=and(condition_1,condition_2,...condition_n)'

      --caslib-filter stringArray   filters for retrieving caslibs, https://developer.sas.com/rest-apis/casManagement/getCaslibs#query-parameters
      --config-name string          restrict configs to objects with the given definitionName, https://developer.sas.com/rest-apis/configuration/getConfigurations#query-parameters
      --config-service string       restrict configs to objects with the given service, https://developer.sas.com/rest-apis/configuration/getConfigurations#query-parameters
      --filter-file string          path to filter file, this is a YAML file containing documents for caslibs, folders, groups and rules. configs are not supported in filter files (but '--config-name' and '--config-service' can still be used with filter files)
      --folder-filter stringArray   filters for retrieving folders, https://developer.sas.com/rest-apis/folders/getFolders#query-parameters
      --group-filter stringArray    filters for retrieving groups, https://developer.sas.com/rest-apis/identities/getGroups#query-parameters
      --rule-filter stringArray     filters for retrieving rules, https://developer.sas.com/rest-apis/authorization/getRules#query-parameters


Use "viyactl [command] --help" for more information about a command.
```

## Setting credentials
### Authinfo
Viyactl can be provided credentials using an authinfo file
```netrc
# Path: .authinfo
# All domains used are example domains
default user sasboot password CorrectHorseBatteryStaple
host https://prod.example.sas.com user viya_admin password Password1
host https://test.example.sas.com user viya_user password Dolphins
host https://qa.example.sas.com client-id uaa_user client-secret hunter2
```

Credentials are provided using the `--auth-info` flag, e.g.
```sh
viyactl read https://prod.example.sas.com --output-directory ./prod --auth-info ./.authinfo
```
- viyactl will use client-id and client-secret if both client-id:client-secret and username:password are defined for a domain
- Specific domains (e.g. test.example.sas.com) take precedence over the default value

### Environment
> Given a Viya deployment at "https://sas.example.com" set SAS_EXAMPLE_COM_USERNAME" and "SAS_EXAMPLE_COM_PASSWORD" or SAS_EXAMPLE_COM_CLIENT_ID" and "SAS_EXAMPLE_COM_CLIENT_SECRET"
> If these environment variables are not set viyactl will use "USERNAME" and "PASSWORD" or CLIENT_ID and CLIENT_SECRET for authentication

#### If all environments have the same credentials
```sh
export USERNAME=viya_admin
export PASSWORD=Password
# or
export CLIENT_ID=viya_admin
export CLIENT_SECRET=VerySecret
```

#### If environments have different credentials

Specific credentials can be used by setting an environment variable corresponding to the fully qualified domain name.
e.g. for https://a.sas.com Viyactl performs the following operations
| Action | Result|
| ------ | ------ |
| cut after ://        | a.sas.com       |
|replace the . characters with _         | a_sas_com       |
| change to upper case        |A_SAS_COM        |
|  add _USERNAME for username  _PASSWORD for password  _CLIENT_ID for client-id  _CLIENT_SECRET for client-secret     |A_SAS_COM_USERNAME  A_SAS_COM_PASSWORD  A_SAS_COM_CLIENT_ID    A_SAS_COM_CLIENT_SECRET        |


These values can then be set as follows (for bash)
```sh
export A_SAS_COM_USERNAME=viya_admin
export A_SAS_COM_PASSWORD=Password
export B_SAS_COM_USERNAME=admin_viya
export B_SAS_COM_PASSWORD=drowssaP
```

## Next steps
- [Reading](./Individual-tutorials/Reading)
- [Reports](./Individual-tutorials/Reports)
- [Altering&Adding Individual Settings](./Individual-tutorials/Altering&Adding-Individual-Settings)
