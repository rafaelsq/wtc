package main

// Config defines the options for watching files
type Config struct {
	NoTrace  bool     `yaml:"no_trace"`
	Ignore   string   `yaml:"ignore"`
	Debounce int      `yaml:"debounce"`
	Rules    []*Rule  `yaml:"rules"`
	Trig     []string `yaml:"trig"`
}

// Rule defines the options for running commands
type Rule struct {
	Name     string   `yaml:"name"`
	Match    string   `yaml:"match"`
	Ignore   string   `yaml:"ignore"`
	Debounce *int     `yaml:"debounce"`
	Command  string   `yaml:"command"`
	Trig     []string `yaml:"trig"`
}
