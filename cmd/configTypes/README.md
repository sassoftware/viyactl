# ConfigTypes

This package is the mechanism that Viyactl uses to be able to manage multiple types of configuration (caslibs, configs, folders, groups and rules), and aims to be easily extensible.

## Adding a new configuration type to viyactl
Create a struct which represents a configuration type:
```go
type ExampleConfigType struct {
	Example any
}
```

Implement the `ConfigType` interface in [./configType.go]:
```go
func (*ExampleConfigType) Name() string {
	return "example"
}

// ...

func (*ExampleConfigType) Clone() ConfigType {
	return &ExampleConfigType{}
}
```

Finally add an init function which adds the new `ConfigType` to SupportedTypes:
```go
func init() {
	SupportedTypes = append(SupportedTypes, &ExampleConfigType{})
}
```

This will then automatically be picked up when viyactl is next built, and will automatically add to all available commands.

## Logging
viyactl uses [zap](https://github.com/uber-go/zap) for logging, to use this in your code:
```go
logger := zap.S()
logger.Infow("Logged!", "number", 5)
```
