Reading is performed using:
```sh
viyactl read $ENVIRONMENT
```
Which will print all settings from the environment (from the readers listed below).
- The `--output-directory`/`-o` flag can be used to specify a directory to write to.
- Read has subcommands to read only 1 given config type (e.g. `viyactl read configs $ENVIRONMENT`)

# Caslibs
Contains content found in $environment/SASDataExplorer

## Schema
```yaml
caslibs:
  $CASLIB_NAME: # string
    type: # string
    path: # string
    scope: # string
    attributes: # map[string]any
    description: # string
    cas_server: # string
    permissions:
      $PRINCIPAL_TYPE: # oneof(user,group,authenticatedUsers,everyone)
        $PRINCIPAL: # oneof(grant,prohibit)
          - $PERMISSIONS # List of permissions
```

# Config
Contains content found in $environment/SASEnvironmentManager Configuration

## Schema
```yaml
config:
  $SERVICE:
    $MEDIA_TYPE:
      $CONFIG: $SETTING

```

# Folders
Contains content found in $environment/SASEnvironmentManager Content

Controls are the rules pertaining to the folder

## Schema
```yaml
folders:
  - path: # string, required
    description: #string
    controls:
      $RULE_NAME:
        $PRINCIPAL_TYPE: # oneof(user,group,authenticatedUsers,everyone)
          $RULE_TYPE: # oneof(grant,prohibit)
            - $PERMISSIONS # List of permissions
```

# Groups
Contains content found in $environment/SASEnvironmentManager Users and Groups

Only custom groups are handled
## Schema
```yaml
groups:
  - $GROUP_ID:
      name: # string
      description: # string
      members:
        - $MEMBERS # List of groups/users

```

# Rules
Contains content found in $environment/SASEnvironmentManager Rules

Rules is filtered to not contain folders

## Schema
```yaml
rules:
  $ObjectURI:
    $PRINCIPAL_TYPE: # oneof(user,group,authenticatedUsers,everyone)
      $PRINCIPAL: # either a principal the rules apply to or "" if applies globally
        - reason: # string
          condition: # string
          description: # string
          permissions:
            $RULE_TYPE: # oneof(grant,prohibit)
              - $PERMISSIONS # List of permissions
```
