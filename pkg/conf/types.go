package conf

type Config struct {
	Installations []Installation `mapstructure:"installations"`
	Teleport      Teleport       `mapstructure:"teleport"`
}

// Settings for a Giant Swarm installation
type Installation struct {
	// The common name of the installation
	Name string `mapstructure:"name"`
	// The base domain associated with the installation
	Domain string `mapstructure:"domain"`
}

// Configuration settings needed for Teleport
type Teleport struct {
	// The string passed to the `--proxy` flag in `tsh login`
	Proxy string `mapstructure:"proxy"`
	// The string passed to the `--auth` flag in `tsh login`
	Auth string `mapstructure:"auth"`
}
