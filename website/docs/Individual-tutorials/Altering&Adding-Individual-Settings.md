---
title: Altering & Adding Individual Settings
---

To change individual settings, simply create a directory with files with the required settings inside:
```
config
├── sitecaslibs.yaml
├── siteconfig.yaml
├── sitefolders.yaml
├── sitegroups.yaml
└── siterules.yaml
```
> If you are only altering one setting type (caslibs/config/folders/groups/rules), you only need the relevant file in the folder, then use the corresponding overwrite subcommand

For this example, we will be changing the global.htmlcommons.disableWelcomeScreens setting:

Inside the created directory, create the required configuration, e.g.:
```yaml
# Path: config/siteconfig.yaml
config:
  global:
    htmlcommons:
      disableWelcomeScreens: "true"
```


Then apply using overwrite, or one of its subcommands:
```sh
# As we are only changing configs, use overwrite configs to disable other readers/writers
viyactl overwrite configs config https://a.sas.com --auth-info .authinfo
```

Now the applied settings should be visible in SAS Data explorer, if the setting did not previously exist it will be created
