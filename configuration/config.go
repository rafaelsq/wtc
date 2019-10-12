package configuration

type Config struct {
	NoTrace  bool    `yaml:"no_trace"`
	Ignore   *string `yaml:"ignore"`
	Debounce int     `yaml:"debounce"`
	Rules    []*Rule `yaml:"rules"`
	Trig     *string `yaml:"trig"`
}

type Rule struct {
	Name     string  `yaml:"name"`
	Match    string  `yaml:"match"`
	Ignore   *string `yaml:"ignore"`
	Debounce int     `yaml:"debounce"`
	Command  string  `yaml:"command"`
	Trig     *string `yaml:"trig"`
}
