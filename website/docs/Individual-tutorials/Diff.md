Diff prints a formatted difference between 2 environments to stdout
```sh
viyactl diff https://a.example.sas.com https://b.example.sas.com
```

```diff
...

rules./audit/activities/*.authenticatedUsers
- one list entry removed:                                       + one list entry added:
- condition: "currentUser() == #activity?.user"                   - condition: "currentUser() == #activity?.user and #activity?.state=='success'"
│ description: "Users can access their own activity records."     │ description: "Users can access their own activity records."
│ permissions:                                                    │ permissions:
│ │ grant:                                                        │ │ grant:
│ │ - read                                                        │ │ - read

rules./dataConnections/connections/*.authenticatedUsers.0.permissions.grant
- one list entry removed:
- secure

rules./dataSources/providers/*/sourceDefinitions/*.authenticatedUsers
- one list entry removed:
- condition: "currentUser() == #sourceDefinition?.createdBy"
│ description: "Only creators can modify, delete and manage permissions on their source definitions."
│ permissions:
│ │ grant:
│ │ - delete
│ │ - read
│ │ - secure
│ │ - update

+ one list entry added:
  - condition: "currentUser() == #sourceDefinition?.createdBy"
  │ description: "Only creators can modify, delete and manage permissions on their source definitions."
  │ permissions:
  │ │ grant:
  │ │ - delete
  │ │ - read
  │ │ - update


rules./launcher/contexts/*.user
- one map entry removed:
sas.forecasting:
- reason: "The Forecasting service can manage the SAS Visual Forecasting launcher context."
│ condition: "'SAS Visual Forecasting launcher context' == #context?.name"
│ description: "The Forecasting service can manage the SAS Visual Forecasting launcher context."
│ permissions:
│ │ grant:
│ │ - delete
│ │ - update

rules./microanalyticScore/modules
- one map entry removed:
user:
│ sas.decisionsFramework:
│ - reason: "Access for bootstrap packages for this service."
│ │ permissions:
│ │ │ grant:
│ │ │ - create
│ │ │ - delete
│ │ │ - secure
│ │ │ - add
│ │ │ - read
│ │ │ - update
│ │ │ - remove

29 Diffs
```
