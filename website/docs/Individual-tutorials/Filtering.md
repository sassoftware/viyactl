
Viyactl supports filters for reading and reporting.

Filtering uses a filter file which contains any combination of caslibs, configs, folders, groups and rules with each one being a YAML 'document'.
```yaml
# Path: filter.yaml
rules:
  /SASDataStudio/**:
    group:
      DataBuilders:
---
folders:
  - path: Public
  - path: Business Glossary
---
groups:
  - SASAdministrators:
      name: SAS Administrators
  - DataBuilders:
      name: Data Builders
---
caslibs:
  - CASUSER(viya_admin):
  - example-connection:
```

Each item may be excluded (in which case all values for that configuration type are included), or incomplete in which case anything under the included paths is included, e.g.:
```yaml
---
configs:
  microanalyticScore:
    jvm:
      java_option_xmx:
      java_option_xss:
---
rules:
  /modelPublish/destinations/**:
    user:
```

Would retrieve:

- All caslibs
- The microanalyticScore.jvm.java_option_x(ms|ss) configs
- All folders
- All groups
- The /modelPublish/destinations/** rules with a principalType of user

Filtering is built into the REST API queries where possible to reduce the number of REST requests necessary to retrieve data
> Filtering of configurations is not properly supported by the REST API - This means that filtering configs does not reduce time


To perform a read with a filter simply supply the path to the filter file
```sh
viyactl read https://dev.example.sas.com --filter-file filter.yaml
```

