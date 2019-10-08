package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/rjeczalik/notify"
)

const debounceTimeout = time.Millisecond * 500

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

type Task struct {
	name  string
	match *regexp.Regexp
	cmd   []string
	fn    func(Task, string, string) error
}

func simpleTask(task Task, _, _ string) error {
	return run(getContext(task.name), nil, task.cmd[0], task.cmd[1:]...)
}

var buildCMD = []string{"-mod=vendor", "-o", "app", "main.go"}
var runCMD = []string{"./app"}

func main() {
	if len(os.Args) > 1 {
		buildCMD = strings.Split(os.Args[1], " ")
		if len(os.Args) > 2 {
			runCMD = strings.Split(os.Args[2], " ")
		}
	}

	dir, _ := os.Getwd()

	isTest := regexp.MustCompile(`_test\.go$`)
	tasks := []Task{
		{"run", regexp.MustCompile(`\.go$`), nil, func(t Task, _, file string) error {
			if isTest.MatchString(file) {
				return nil
			}

			return buildNRun(getContext(t.name))
		}},
		{"gqlgen", regexp.MustCompile("schema.graphql$"), []string{"go", "run", "github.com/99designs/gqlgen"}, simpleTask},
		{"generate", regexp.MustCompile("pkg/iface/"), []string{"go", "generate", "./..."}, simpleTask},
		{"proto", regexp.MustCompile(".proto$"), []string{"make", "proto"}, simpleTask},
		{"test", isTest, nil, func(t Task, pkg, _ string) error {
			log := func() {
				fmt.Printf("\x1b[38;5;239m[%s]\x1b[0m \x1b[38;5;2mTesting\x1b[0m %s/...\n",
					time.Now().Format("15:04:05"), pkg[len(dir):])
			}
			return run(getContext(t.name), &log, "go", "test", "-mod=vendor", "-cover", pkg)
		}},
		{"lint", regexp.MustCompile(`\.go$`), nil, func(t Task, pkg, _ string) error {
			_ = run(getContext(t.name), nil, "golangci-lint", "run", pkg)
			return nil
		}},
	}

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
		if strings.HasPrefix(pkg, dir+"/vendor/") || strings.HasPrefix(pkg, dir+"/.git") {
			continue
		}

		for _, task := range tasks {
			task := task
			if task.match.MatchString(path) {
				go func() {
					if err := task.fn(task, pkg, path); err != nil {
						log.Printf("%s failed; %v\n", task.name, err)
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

	err := run(ctx, &print, "go", append([]string{"build"}, buildCMD...)...)
	if err != nil {
		return fmt.Errorf("build failed; %w", err)
	}

	if ctx.Err() == nil {
		fmt.Printf("\x1b[38;5;239m[%s]\x1b[0m \x1b[38;5;243mBuilded %s\x1b[0m\n", time.Now().Format("15:04:05"),
			time.Since(start))
		return run(ctx, nil, runCMD[0], runCMD[1:]...)
	}

	return nil
}

func run(ctx context.Context, onStart *func(), command string, args ...string) error {
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(debounceTimeout):
	}

	if onStart != nil {
		(*onStart)()
	}

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
