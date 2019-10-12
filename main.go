package main

import (
	"context"
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

	"github.com/rafaelsq/wtc/configuration"
	"github.com/rjeczalik/notify"
	yaml "gopkg.in/yaml.v2"
)

var config configuration.Config

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

func main() {
	buildCMD := "go build"
	runCMD := "./$(basename `pwd`)"

	if len(os.Args) > 1 {
		buildCMD = os.Args[1]
		if len(os.Args) > 2 {
			runCMD = os.Args[2]
		}
	}

	config = configuration.Config{
		Debounce: 300,
		Ignore:   &[]string{`\.git/`}[0],
		Rules:    []*configuration.Rule{},
	}

	yamlFile, err := ioutil.ReadFile("wtc.yaml")
	if err != nil {
		yamlFile, err = ioutil.ReadFile(".wtc.yaml")
	}
	if err != nil {
		config.Trig = &[]string{"build"}[0]
		config.Rules = append(config.Rules, &configuration.Rule{
			Name:    "run",
			Match:   `^$`,
			Command: runCMD,
		})

		config.Rules = append(config.Rules, &configuration.Rule{
			Name:     "build",
			Match:    `\.go$`,
			Debounce: config.Debounce,
			Ignore:   &[]string{`_test\.go$`}[0],
			Command:  buildCMD,
			Trig:     &[]string{"run"}[0],
		})

		config.Rules = append(config.Rules, &configuration.Rule{
			Name:     "test",
			Match:    `_test\.go$`,
			Debounce: config.Debounce,
			Command:  "go test -cover {PKG}",
		})
	} else {
		err = yaml.Unmarshal(yamlFile, &config)
		if err != nil {
			log.Fatalf("Invalid wtc.yaml: %v", err)
		}
	}

	for _, rule := range config.Rules {
		if rule.Debounce == 0 {
			rule.Debounce = config.Debounce
		}
	}

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
		if config.Ignore != nil {
			if retrieveRegexp(*config.Ignore).MatchString(path) {
				continue
			}
		}

		for _, rule := range config.Rules {
			rule := rule

			if rule.Ignore != nil && retrieveRegexp(*rule.Ignore).MatchString(path) {
				continue
			}
			if retrieveRegexp(rule.Match).MatchString(path) {
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

func trig(rule *configuration.Rule, pkg, path string) error {
	ctx := getContext(rule.Name)

	select {
	case <-ctx.Done():
		return nil
	case <-time.After(time.Duration(rule.Debounce) * time.Millisecond):
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
