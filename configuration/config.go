package configuration

type Config struct {
	Build string `yaml:"build"`
	Run   string `yaml:"run"`
	Rules []Rule `yaml:"rules"`
}

type Rule struct {
	Name     string `yaml:"name"`
	Regex    string `yaml:"regex"`
	Ignore   string `yaml:"ignore"`
	Debounce int    `yaml:"debounce"`
	Command  string `yaml:"command"`
}

func (c *Config) Test() *Rule {
	for _, rule := range c.Rules {
		if rule.Name == "Test" {
			return &rule
		}
	}
	panic("Must have a rule named Test")
}
