# Generating
Reports can be generated with the generate-report command

Reports require a minimum of 2 environments to be specified, but typically a diff will be more useful for cases where exactly 2 environments are specified.
```sh
viyactl generate-report ./example-config/ https://dev.example.sas.com https://prod.example.sas.com
```

This will produce an output similar to:
```sh
✔   Report created at ./viyactl-report-11-41_08-10-2025.html
```

# Reading the Report
```sh
# Assuming xdg-utils is installed, otherwise navigate and open
open ./viyactl-report-11-41_08-10-2025.html
```

This will open a report in a browser window:
- Clicking a gold square will show the base env
- Clicking on a red square shows the difference between the base and the clicked env.
- Clicking another env will hide the original diff and show the diff between the base and the newly clicked env.
- Clicking an already displayed env again will hide the diffs.
