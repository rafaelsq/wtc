package wtc

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rjeczalik/notify"
	yaml "gopkg.in/yaml.v2"
)

var appContext context.Context
var contexts map[string]context.CancelFunc
var contextsLock map[string]chan struct{}
var ctxmutex sync.Mutex
var contextsLockMutext sync.Mutex

func getContext(label string) (context.Context, context.CancelFunc) {
	ctxmutex.Lock()
	defer ctxmutex.Unlock()

	if cancel, has := contexts[label]; has {
		cancel()
	}

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(appContext)
	contexts[label] = cancel
	return ctx, cancel
}

var config *Config

func ParseArgs() *Config {
	flag.CommandLine.Usage = func() {
		fmt.Fprintf(
			flag.CommandLine.Output(),
			"USAGE:\n$ wtc [[flags] regex command]\n\n"+
				"If [.]wtc.y[a]ml exists, it will be used.\n\n"+
				"FLAGS:\n",
		)
		flag.PrintDefaults()
	}

	config := &Config{Debounce: 300}

	flag.IntVar(&config.Debounce, "debounce", 300, "global debounce")
	flag.StringVar(&config.Ignore, "ignore", "", "regex")
	flag.BoolVar(&config.NoTrace, "no-trace", false, "disable messages.")

	flag.Parse()

	if has, err := readConfig(config); err != nil {
		log.Fatal(err)
	} else if !has && flag.NArg() < 2 {
		fmt.Fprintf(os.Stderr, "No [.]wtc.yaml or valid command provided.\n")
		flag.CommandLine.Usage()
		return nil
	} else if !has {
		config.Rules = append(config.Rules, &Rule{
			Name:    "run",
			Match:   flag.Arg(0),
			Command: flag.Arg(1),
		})
	}

	return config
}

func Start(cfg *Config) {
	var cancelAll context.CancelFunc

	config = cfg
	appContext, cancelAll = context.WithCancel(context.Background())
	contexts = make(map[string]context.CancelFunc)
	contextsLock = make(map[string]chan struct{})

	c := make(chan notify.EventInfo)

	if err := notify.Watch("./...", c, notify.All); err != nil {
		log.Fatal(err)
	}

	dir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	go findAndTrig(config.Trig, "./", "./")

	exitSignal := make(chan os.Signal, 1)
	signal.Notify(exitSignal, os.Interrupt)

	for {
		select {
		case <-exitSignal:
			notify.Stop(c)
			cancelAll()

			for _, r := range config.Rules {
				contextsLockMutext.Lock()
				if l, exists := contextsLock[r.Name]; exists {
					contextsLockMutext.Unlock()
					l <- struct{}{}
					<-l
					continue
				}
				contextsLockMutext.Unlock()
			}
			return
		case ei := <-c:
			path := ei.Path()
			pieces := strings.Split("."+strings.Split(path, dir)[1], "/")
			pkg := strings.Join(pieces[:len(pieces)-1], "/")

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
}

func findFile() ([]byte, error) {
	for _, file := range []string{"wtc.yaml", ".wtc.yaml", "wtc.yml", ".wtc.yml"} {
		if _, err := os.Stat(file); err == nil {
			return ioutil.ReadFile(file)
		}
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

func envToStrings(env []*Env) []string {
	result := []string{}
	for _, e := range env {
		result = append(result, e.Name+"="+e.Value)
	}
	return result
}

func findAndTrig(key []string, pkg, path string) {
	for _, s := range key {
		for _, r := range config.Rules {
			if r.Name == s {
				r := r
				go func() {
					if err := trig(r, pkg, path); err != nil {
						fmt.Printf("\033[30;1m[%s] \033[31;1m[%s failed]\033[0m \033[30;1m%s\033[0m\n",
							time.Now().Format("15:04:05"), r.Name, err)
					}
				}()
				break
			}
		}
	}
}

func trig(rule *Rule, pkg, path string) error {
	ctx, cancel := getContext(rule.Name)

	contextsLockMutext.Lock()
	var queue chan struct{}
	var has bool
	queue, has = contextsLock[rule.Name]
	if !has {
		queue = make(chan struct{}, 1)
		contextsLock[rule.Name] = queue
	}
	contextsLockMutext.Unlock()

	queue <- struct{}{}
	defer func() {
		<-queue
	}()

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

	env := os.Environ()
	env = append(env, envToStrings(config.Env)...)
	env = append(env, envToStrings(rule.Env)...)

	if !config.NoTrace {
		fmt.Printf("\033[30;1m[%s] \033[32;1m[%s]\033[0m \033[30;3m%s\033[0m\n",
			time.Now().Format("15:04:05"), rule.Name, cmd)
	}

	err := run(ctx, cmd, env)
	if err == context.Canceled {
		return nil
	}

	defer cancel()
	if err != nil {
		return err
	}

	findAndTrig(rule.Trig, pkg, path)

	return nil
}

func run(ctx context.Context, command string, env []string) error {
	cmd := exec.Command("sh", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env

	// ask Go to create a new Process Group for this process
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	err := cmd.Start()
	if err != nil {
		return err
	}

	done := make(chan struct{})
	exit := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			// Process Group will use the same ID as this process.
			// Kill the process group(minus)
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		case <-done:
		}
		close(exit)
	}()

	err = cmd.Wait()
	if err != nil && uint32(cmd.ProcessState.Sys().(syscall.WaitStatus)) == uint32(syscall.SIGKILL) {
		err = context.Canceled
	}

	close(done)
	<-exit
	return err
}
