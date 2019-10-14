package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rjeczalik/notify"
	yaml "gopkg.in/yaml.v2"
)

var contexts map[string]context.CancelFunc
var ctxmutex sync.Mutex

func getContext(label string) context.Context {
	ctxmutex.Lock()
	defer ctxmutex.Unlock()

	if cancel, has := contexts[label]; has {
		cancel()
	}

	var ctx context.Context
	ctx, contexts[label] = context.WithCancel(context.Background())
	return ctx
}

var config *Config

func main() {
	flag.CommandLine.Usage = func() {
		fmt.Fprintf(
			flag.CommandLine.Output(),
			"USAGE:\n$ wtc [[flags] regex command]\n\n"+
				"If [.]wtc.yaml exists, it will be used.\n\n"+
				"FLAGS:\n",
		)
		flag.PrintDefaults()
	}

	config = &Config{Debounce: 300}

	flag.IntVar(&config.Debounce, "debounce", 300, "global debounce")
	flag.StringVar(&config.Ignore, "ignore", "", "regex")
	flag.BoolVar(&config.NoTrace, "no-trace", false, "disable messages.")

	flag.Parse()

	if has, err := readConfig(config); err != nil {
		log.Fatal(err)
	} else if !has && flag.NArg() < 2 {
		fmt.Fprintf(os.Stderr, "No [.]wtc.yaml or valid command provided.\n")
		flag.CommandLine.Usage()
		return
	} else {
		config.Rules = append(config.Rules, &Rule{
			Name:    "run",
			Match:   flag.Arg(0),
			Command: flag.Arg(1),
		})
	}

	start(config)
}

func start(config *Config) {
	contexts = make(map[string]context.CancelFunc)

	c := make(chan notify.EventInfo)

	if err := notify.Watch("./...", c, notify.All); err != nil {
		log.Fatal(err)
	}
	defer notify.Stop(c)

	dir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	if config.Trig != nil {
		go findAndTrig(*config.Trig, "./", "./")
	}
	for ei := range c {
		path := ei.Path()
		pieces := strings.Split("."+strings.Split(path, dir)[1], "/")
		pkg := strings.Join(pieces[:len(pieces)-1], "/")

		// ignore
		if config.Ignore != "" {
			if retrieveRegexp(config.Ignore).MatchString(path) {
				continue
			}
		}

		for _, rule := range config.Rules {
			rule := rule

			if rule.Ignore != "" && retrieveRegexp(rule.Ignore).MatchString(path) {
				continue
			}

			if rule.Match != "" && retrieveRegexp(rule.Match).MatchString(path) {
				go func() {
					if err := trig(rule, pkg, path); err != nil {
						fmt.Printf("\033[30;1m[%s] \033[31;1m[%s failed]\033[0m \033[30;1m%s\033[0m\n",
							time.Now().Format("15:04:05"), rule.Name, err)
					}
				}()
			}
		}
	}
}

func findFile() ([]byte, error) {
	if _, err := os.Stat("wtc.yaml"); err == nil {
		return ioutil.ReadFile("wtc.yaml")
	}

	if _, err := os.Stat(".wtc.yaml"); err == nil {
		return ioutil.ReadFile(".wtc.yaml")
	}

	return nil, nil
}

func readConfig(config *Config) (bool, error) {
	yamlFile, err := findFile()
	if err != nil {
		return false, err
	}

	if len(yamlFile) != 0 {
		return true, yaml.Unmarshal(yamlFile, &config)
	}

	return false, nil
}

var regexpMutex = &sync.Mutex{}
var regexpMap = map[string]*regexp.Regexp{}

func retrieveRegexp(pattern string) *regexp.Regexp {
	regexpMutex.Lock()
	var result, ok = regexpMap[pattern]
	if !ok {
		result = regexp.MustCompile(pattern)
		regexpMap[pattern] = result
	}
	regexpMutex.Unlock()
	return result
}

func findAndTrig(key, pkg, path string) {
	for _, r := range config.Rules {
		if r.Name == key {
			if err := trig(r, pkg, path); err != nil {
				fmt.Printf("\033[30;1m[%s] \033[31;1m[%s failed]\033[0m \033[30;1m%s\033[0m\n",
					time.Now().Format("15:04:05"), r.Name, err)
			}
			return
		}
	}
}

func trig(rule *Rule, pkg, path string) error {
	ctx := getContext(rule.Name)

	debounce := config.Debounce
	if rule.Debounce != nil {
		debounce = *rule.Debounce
	}

	select {
	case <-ctx.Done():
		return nil
	case <-time.After(time.Duration(debounce) * time.Millisecond):
	}

	cmd := strings.Replace(strings.Replace(rule.Command, "{PKG}", pkg, -1), "{FILE}", path, -1)

	if !config.NoTrace {
		fmt.Printf("\033[30;1m[%s] \033[32;1m[%s]\033[0m \033[30;3m%s\033[0m\n",
			time.Now().Format("15:04:05"), rule.Name, cmd)
	}

	err := run(ctx, cmd)
	if err == context.Canceled {
		return nil
	}
	if err != nil {
		return err
	}

	if rule.Trig != nil {
		findAndTrig(*rule.Trig, pkg, path)
	}

	return nil
}

func run(ctx context.Context, command string) error {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Start()
	if err != nil {
		return err
	}

	err = cmd.Wait()
	if err != nil {
		if uint32(cmd.ProcessState.Sys().(syscall.WaitStatus)) == uint32(syscall.SIGKILL) {
			return context.Canceled
		}

		return err
	}

	return nil
}
