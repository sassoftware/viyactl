---
title: Migrating Environments
---
Viyactl can migrate settings from one environment to any number of environments

Viyactl overwrite accepts a minimum of two arguments, where the first argument is the base environment and subsequent environments get the base written to them.

For example:
```sh
viyactl overwrite https://a.example.sas.com https://b.example.sas.com https://c.example.sas.com https://d.example.sas.com
```
Will write the settings of https://a.example.sas.com to all the other environments

## Saving environments before write
To save the state of the environments before writing to them use the `--output` flag
This will create a directory for each environment that gets written to corresponding to the fully qualified domain name.
e.g. for https://a.sas.com Viyactl performs the following operations
| Action                         | Result    |
| ------                         | ------    |
| cut after ://                  | a.sas.com |
|replace the . characters with - | a-sas-com |
| change to lower case           | a-sas-com |

So the command:
```sh
viyactl overwrite https://a.example.sas.com https://b.example.sas.com https://c.example.sas.com https://d.example.sas.com --output
```
Will create:
```
.
├── b-example-sas-com
│   ├── sitecaslibs.yaml
│   ├── siteconfig.yaml
│   ├── sitefolders.yaml
│   ├── sitegroups.yaml
│   └── siterules.yaml
├── c-example-sas-com
│   ├── sitecaslibs.yaml
│   ├── siteconfig.yaml
│   ├── sitefolders.yaml
│   ├── sitegroups.yaml
│   └── siterules.yaml
├── d-example-sas-com
│   ├── sitecaslibs.yaml
│   ├── siteconfig.yaml
│   ├── sitefolders.yaml
│   ├── sitegroups.yaml
│   └── siterules.yaml
...
```
Where each file is the pre-overwrite values. This allows for easy rollback if any issues occurred.
