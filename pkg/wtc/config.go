package wtc

// Config defines the options for watching files
type Config struct {
	NoTrace  bool     `yaml:"no_trace"`
	Ignore   string   `yaml:"ignore"`
	Debounce int      `yaml:"debounce"`
	Rules    []*Rule  `yaml:"rules"`
	Trig     []string `yaml:"trig"`
	Env      []*Env   `yaml:"env"`
	EnvFile  string   `yaml:"env_file"`
	Format   struct {
		OK   string `yaml:"ok"`
		Fail string `yaml:"fail"`
	} `yaml:"format"`
}

// Rule defines the options for running commands
type Rule struct {
	Name     string   `yaml:"name"`
	Match    string   `yaml:"match"`
	Ignore   string   `yaml:"ignore"`
	Debounce *int     `yaml:"debounce"`
	Command  string   `yaml:"command"`
	Trig     []string `yaml:"trig"`
	Env      []*Env   `yaml:"env"`
}

// Env defines environment variables
type Env struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}
