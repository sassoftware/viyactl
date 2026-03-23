---
title: Healthcheck
---

> Please understand [Filtering](./Filtering) and [Reports](./Reports) before reading

Reporting can be combined with filtering and/or disabling to produce streamlined reports.
## Disabling readers
By specifying one or more `--disable` flags parts of the report can be removed
```sh
viyactl generate-report config https://prod.example.sas.com https://dev.example.sas.com https://qa.example.sas.com --disable caslibs --disable groups
```

## Filtering
By using the `--filter-file` flag, a filter may be applied to the report
```sh
viyactl generate-report config https://prod.example.sas.com https://dev.example.sas.com https://qa.example.sas.com --filter-file filter.yaml
```
Which will filter all environments (including local ones) using the specified filter.

## Putting it all together
Both of the above functions can be combined to create a report only containing useful information.

For example, we want to check the java option xmx and xss settings for the Micro Analytics Score service across multiple environments.

First create a filter file:
```yaml

# Path: filter.yaml
---
config:
microanalyticScore:
    jvm:
      java_option_xmx:
      java_option_xss:
```

Then simply generate a report with all other readers disabled:
```sh
viyactl generate-report config https://prod.example.sas.com https://dev.example.sas.com https://qa.example.sas.com --disable caslibs --disable folders --disable groups --disable rules --filter-file filter.yaml
```
