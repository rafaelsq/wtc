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
	yamlFile, err := ioutil.ReadFile("wtc.yaml")
	if err != nil {
		log.Fatalf("No wtc.yaml found")
	}
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		log.Fatalf("Invalid wtc.yaml: %v", err)
	}

	dir, _ := os.Getwd()

	contexts = make(map[string]context.CancelFunc)

	c := make(chan notify.EventInfo)

	if err := notify.Watch("./...", c, notify.Create, notify.Write, notify.Remove); err != nil {
		log.Fatal(err)
	}
	defer notify.Stop(c)

	go func() {
		if err := buildNRun(getContext("run")); err != nil {
			log.Println(err)
		}
	}()
	for ei := range c {
		path := ei.Path()
		pieces := strings.Split(path, "/")
		pkg := strings.Join(pieces[:len(pieces)-1], "/")

		// ignore
		if strings.HasPrefix(pkg, dir+"/.git") {
			continue
		}

		for _, rule := range config.Rules {
			task := rule
			match := regexp.MustCompile(task.Regex)
			if match.MatchString(path) {
				go func() {
					if err := run(getContext(task.Name), task, nil); err != nil {
						log.Printf("%s failed; %v\n", task.Name, err)
					}
				}()
			}
		}
	}
}

func buildNRun(ctx context.Context) error {
	start := time.Now()
	print := func() {
		fmt.Printf("\x1b[38;5;239m[%s]\x1b[0m \x1b[38;5;1mBuilding...\x1b[0m\n", start.Format("15:04:05"))
	}
	buildTask := configuration.Rule{
		Name:     "run",
		Debounce: 500,
		Command:  config.Build,
	}
	err := run(ctx, buildTask, &print)
	if err != nil {
		return fmt.Errorf("build failed; %w", err)
	}

	if ctx.Err() == nil {
		fmt.Printf("\x1b[38;5;239m[%s]\x1b[0m \x1b[38;5;243mBuilt %s\x1b[0m\n", time.Now().Format("15:04:05"),
			time.Since(start))
		runTask := configuration.Rule{
			Name:     "run",
			Debounce: 500,
			Command:  config.Run,
		}
		return run(ctx, runTask, nil)
	}

	return nil
}

func run(ctx context.Context, task configuration.Rule, onStart *func()) error {
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(time.Duration(task.Debounce) * time.Millisecond):
	}

	if onStart != nil {
		(*onStart)()
	}

	command := strings.Split(task.Command, " ")[0]
	args := strings.Split(task.Command, " ")[1:]
	cmd := exec.Command(command, args...)
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
	if err != nil {
		return err
	}

	return nil
}
