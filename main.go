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
		Rules:    []configuration.Rule{},
	}

	yamlFile, err := ioutil.ReadFile("wtc.yaml")
	if err != nil {
		config.Trig = &[]string{"build"}[0]
		config.Rules = append(config.Rules, configuration.Rule{
			Name:    "run",
			Match:   `^$`,
			Command: runCMD,
		})

		config.Rules = append(config.Rules, configuration.Rule{
			Name:     "build",
			Match:    `\.go$`,
			Debounce: config.Debounce,
			Ignore:   &[]string{`_test\.go$`}[0],
			Command:  buildCMD,
			Trig:     &[]string{"run"}[0],
		})

		config.Rules = append(config.Rules, configuration.Rule{
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

	if config.Trig != nil {
		go func() {
			dir, _ := os.Getwd()
			findAndTrig(*config.Trig, dir, dir)
		}()
	}
	for ei := range c {
		path := ei.Path()
		pieces := strings.Split(path, "/")
		pkg := strings.Join(pieces[:len(pieces)-1], "/")

		// ignore
		if config.Ignore != nil {
			if regexp.MustCompile(*config.Ignore).MatchString(path) {
				continue
			}
		}

		for _, rule := range config.Rules {
			rule := rule

			if rule.Ignore != nil && regexp.MustCompile(*rule.Ignore).MatchString(path) {
				continue
			}
			if regexp.MustCompile(rule.Match).MatchString(path) {
				go func() {
					if err := trig(rule, pkg, path); err != nil {
						log.Printf("%s failed; %v\n", rule.Name, err)
						return
					}

					if rule.Trig != nil {
						findAndTrig(*rule.Trig, pkg, path)
					}
				}()
			}
		}
	}
}

func findAndTrig(key, pkg, path string) {
	for _, r := range config.Rules {
		if r.Name == key {
			if err := trig(r, pkg, path); err != nil {
				log.Printf("%s failed; %v\n", r.Name, err)
				return
			}
			if r.Trig != nil {
				findAndTrig(*r.Trig, pkg, path)
			}
			return
		}
	}
}

func trig(rule configuration.Rule, pkg, path string) error {
	ctx := getContext(rule.Name)

	select {
	case <-ctx.Done():
		return nil
	case <-time.After(time.Duration(rule.Debounce) * time.Millisecond):
	}

	cmd := strings.Replace(strings.Replace(rule.Command, "{PKG}", pkg, -1), "{FILE}", path, -1)
	return run(ctx, nil, cmd)
}

func run(ctx context.Context, onStart *func(), command string) error {
	if onStart != nil {
		(*onStart)()
	}

	if !config.NoTrace {
		fmt.Printf("\x1b[38;5;239m[%s]\x1b[0m \x1b[38;5;243m%s\x1b[0m\n", time.Now().Format("15:04:05"), command)
	}
	cmd := exec.Command("bash", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Start()
	if err != nil {
		return err
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
		case <-done:
		}
	}()

	err = cmd.Wait()
	if err != nil && ctx.Err() == nil {
		return err
	}

	return nil
}
