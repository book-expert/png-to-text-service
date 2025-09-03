module github.com/nnikolov3/png-to-text-service

go 1.25

require (
	github.com/nnikolov3/configurator v0.0.0
	github.com/nnikolov3/logger v0.0.0
)

require github.com/pelletier/go-toml/v2 v2.2.4 // indirect

replace (
	github.com/nnikolov3/configurator => ../configurator
	github.com/nnikolov3/logger => ../logger
)
