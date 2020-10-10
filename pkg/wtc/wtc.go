package wtc

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
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

	"github.com/radovskyb/watcher"
	"gopkg.in/yaml.v2"
)

var (
	appContext         context.Context
	contexts           map[string]context.CancelFunc
	contextsLock       map[string]chan struct{}
	ctxmutex           sync.Mutex
	contextsLockMutext sync.Mutex
	acquire            chan struct{}
	BR                 = '\n'
)

var (
	logger        chan Rune
	templateRegex = regexp.MustCompile(`\{\{\.([^}]+)\}\}`)
	exportRe      = regexp.MustCompile(`(i?)export\s+`)
	replaceEnvRe  = regexp.MustCompile(`(i?)\%\{[A-Z0-9_]+\}\%`)

	TimeFormat = "15:04:05"

	TypeOK         = "\u001b[38;5;244m[{{.Time}}] \u001b[38;5;2m[{{.Title}}]\u001b[0m \u001b[38;5;238m{{.Message}}\u001b[0m\n"
	TypeFail       = "\u001b[38;5;244m[{{.Time}}] \u001b[38;5;1m[{{.Title}}] \u001b[38;5;238m{{.Message}}\u001b[0m\n"
	TypeCommandOK  = "\u001b[38;5;240m[{{.Time}}] [{{.Title}}] \u001b[0m{{.Message}}\n"
	TypeCommandErr = "\u001b[38;5;240m[{{.Time}}] [{{.Title}}] \u001b[38;5;1m{{.Message}}\u001b[0m\n"
)

type Rune struct {
	Type     string
	Time     string
	Title    string
	Rune     rune
	IsStderr bool
	End      bool
}

func (l *Rune) Log() {
	logger <- *l
}

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
			"USAGE:\nwtc [[flags] [regex command]]\n"+
				"\tex.: wtc\n\t    // will read [.]wtc.y[a]ml\n"+
				"\tex.: wtc \"_test\\.go$\" \"go test -cover {PKG}\"\n\n"+
				"wtc [flags]] [rule-name]\n\tex.: wtc -t rule-name\n\t     wtc --no-trace \"rule ruleb\"\n"+
				"FLAGS:\n",
		)
		flag.PrintDefaults()
	}

	config := &Config{Debounce: 300}

	var configFilePath string

	flag.IntVar(&config.Debounce, "debounce", 300, "global debounce")
	flag.StringVar(&config.Ignore, "ignore", "", "regex")
	flag.BoolVar(&config.NoTrace, "no-trace", false, "disable messages.")
	flag.StringVar(&configFilePath, "f", "", "wtc config file (default try to find [.]wtc.y[a]ml)")

	var trigs string
	flag.StringVar(&trigs, "t", "", "trig one or more rules by name\n\tex.: wtc -t ruleA\n\t     wtc -t \"ruleA ruleB\"")

	flag.Parse()

	if has, err := readConfig(config, configFilePath); err != nil {
		log.Fatal(err)
	} else if has && flag.NArg() == 1 {
		trigs = flag.Arg(0)
	} else if !has && flag.NArg() < 2 {
		_, _ = fmt.Fprintf(os.Stderr, "No [.]wtc.yaml or valid command provided.\n")
		flag.CommandLine.Usage()
		os.Exit(1)
	} else if !has {
		config.Rules = append(config.Rules, &Rule{
			Name:    "run",
			Match:   flag.Arg(0),
			Command: flag.Arg(1),
		})
	}

	if trigs != "" {
		config.Trig = strings.Split(trigs, " ")
		config.ExitOnTrig = true
	}

	if config.Format.OK != "" {
		TypeOK = config.Format.OK
	}

	if config.Format.Fail != "" {
		TypeFail = config.Format.Fail
	}

	if config.Format.CommandOK != "" {
		TypeCommandOK = config.Format.CommandOK
	}

	if config.Format.CommandErr != "" {
		TypeCommandErr = config.Format.CommandErr
	}
	if config.Format.Time != "" {
		TimeFormat = config.Format.Time
	}

	return config
}

func Start(cfg *Config) {
	var cancelAll context.CancelFunc

	config = cfg
	appContext, cancelAll = context.WithCancel(context.Background())
	contexts = make(map[string]context.CancelFunc)
	contextsLock = make(map[string]chan struct{})

	dir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	acquire = make(chan struct{}, 1)
	logger = make(chan Rune, 1)
	go func() {
		var current string
		var currentIsErr *bool
		var currentOut *os.File
		var currentArgs [][]byte
		for r := range logger {

			if r.End {
				current = ""
				currentIsErr = nil
				continue
			}

			if r.Rune == BR || (r.Title != current || (currentIsErr == nil || r.IsStderr != *currentIsErr)) {
				// close current
				if currentOut != nil {
					_, err = (*currentOut).Write(currentArgs[1])
				}

				// parse tpl
				output := []byte(templateRegex.ReplaceAllStringFunc(r.Type, func(k string) string {
					switch k[3:][:len(k)-5] {
					case "Time":
						return time.Now().Format(TimeFormat)
					case "Title":
						return r.Title
					default:
						return k
					}
				}))

				// set current
				current = r.Title
				currentIsErr = &[]bool{r.IsStderr}[0]
				currentArgs = bytes.Split(output, []byte("{{.Message}}"))

				if r.IsStderr {
					currentOut = os.Stderr
				} else {
					currentOut = os.Stdout
				}

				// write start
				_, err = currentOut.Write(currentArgs[0])
			}

			if r.Rune == 0 || r.Rune == BR {
				continue
			}

			_, err = fmt.Fprintf(currentOut, "%c", r.Rune)
		}
	}()

	go func() {
		if config.ExitOnTrig {
			findAndTrig(false, config.Trig, "./", "./")
			os.Exit(0)
		}

		if len(config.TrigAsync) > 0 {
			go findAndTrig(true, config.TrigAsync, "./", "./")
		}
		if len(config.Trig) > 0 {
			findAndTrig(false, config.Trig, "./", "./")
		}
	}()

	w := watcher.New()
	w.FilterOps(watcher.Write, watcher.Remove, watcher.Create)

	exitSignal := make(chan os.Signal, 1)
	signal.Notify(exitSignal, syscall.SIGINT, syscall.SIGTERM)

	exit := make(chan struct{})

	go func() {
		defer close(exit)
		for {
			select {
			case err := <-w.Error:
				log.Fatal(err)
			case <-w.Closed:
				return
			case <-exitSignal:
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

				w.Close()
				return
			case e := <-w.Event:
				if e.IsDir() {
					continue
				}

				path := e.Path

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
								Log(rule.Name, TypeFail, err.Error(), true)
							}
						}()
					}
				}
			}
		}
	}()

	if err := w.AddRecursive("."); err != nil {
		log.Fatalln(err)
	}

	if err := w.Start(time.Millisecond * 100); err != nil {
		log.Fatalln(err)
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

func readConfig(config *Config, filePath string) (bool, error) {
	var yamlFile []byte
	var err error
	if len(filePath) != 0 {
		yamlFile, err = ioutil.ReadFile(filePath)
	} else {
		yamlFile, err = findFile()
	}
	if err != nil {
		return false, err
	}

	if len(yamlFile) != 0 {
		envs := os.Environ()
		keys := make(map[string]string, len(envs))
		for _, v := range envs {
			pieces := strings.Split(v, "=")
			keys[pieces[0]] = pieces[1]
		}

		replaceEnvRe := regexp.MustCompile(`(i?)\%\{[A-Z0-9_]+\}\%`)

		yamlFile = []byte(replaceEnvRe.ReplaceAllStringFunc(string(yamlFile), func(k string) string {
			return keys[strings.TrimSuffix(strings.TrimPrefix(k, "%{"), "}%")]
		}))

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

func Log(name, tpe, msg string, isStderr bool) {

	acquire <- struct{}{}
	defer func() {
		<-acquire
	}()

	for _, c := range msg {
		(&Rune{
			Type:     tpe,
			Title:    name,
			Rune:     c,
			IsStderr: isStderr,
		}).Log()
	}

	(&Rune{
		Type:     tpe,
		Title:    name,
		IsStderr: isStderr,
		End:      true,
	}).Log()
}

func findAndTrig(async bool, key []string, pkg, path string) {
	var wg sync.WaitGroup
	for _, s := range key {
		found := false
		for _, r := range config.Rules {
			if r.Name == s {
				r := r
				fn := func() {
					if err := trig(r, pkg, path); err != nil {
						Log(r.Name, TypeFail, err.Error(), true)
					}
				}

				if async {
					wg.Add(1)
					go func() {
						defer wg.Done()
						fn()
					}()
				} else {
					fn()
				}

				found = true
				break
			}
		}

		if !found {
			Log(s, TypeFail, "rule not found", true)
		}
	}

	wg.Wait()
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

	debounce := config.Debounce
	if rule.Debounce != nil {
		debounce = *rule.Debounce
	}

	select {
	case <-ctx.Done():
		<-queue
		return nil
	case <-time.After(time.Duration(debounce) * time.Millisecond):
	}

	cmd := strings.Replace(strings.Replace(rule.Command, "{PKG}", pkg, -1), "{FILE}", path, -1)

	keys := map[string]string{}
	envs := os.Environ()
	for _, v := range envs {
		pieces := strings.Split(v, "=")
		keys[pieces[0]] = pieces[1]
	}
	for _, e := range append(config.Env, rule.Env...) {
		if e.Type == "file" {
			b, err := ioutil.ReadFile(e.Name)
			if err != nil {
				panic(err)
			}

			body := replaceEnvRe.ReplaceAllStringFunc(string(b), func(k string) string {
				return keys[strings.TrimSuffix(strings.TrimPrefix(k, "%{"), "}%")]
			})

			for _, l := range strings.Split(body, "\n") {
				l := strings.TrimSpace(l)
				if len(l) > 0 && l[0] != '#' {
					pieces := strings.Split(exportRe.ReplaceAllString(l, ""), "=")
					if len(pieces) > 1 {
						keys[strings.TrimSpace(pieces[0])] = pieces[1]
					}
				}
			}
		} else {
			keys[strings.TrimSpace(e.Name)] = strings.TrimSpace(e.Value)
		}
	}

	for k, v := range keys {
		envs = append(envs, k+"="+strings.Trim(v, "\" "))
	}

	if !config.NoTrace {
		Log(rule.Name, TypeOK, cmd, false)
	}

	err := run(ctx, rule.Name, cmd, envs)
	if err == context.Canceled {
		<-queue
		return nil
	}

	if err != nil {
		<-queue
		cancel()
		return err
	}

	<-queue
	cancel()

	if len(rule.TrigAsync) > 0 {
		go findAndTrig(true, rule.TrigAsync, pkg, path)
	}

	if len(rule.Trig) > 0 {
		findAndTrig(false, rule.Trig, pkg, path)
	}

	return nil
}

func pipeChar(tpe, id string, isStderr bool) io.WriteCloser {
	rr, ww := io.Pipe()

	reader := bufio.NewReader(rr)
	go func() {
		defer rr.Close()

		me := false

		cancel := make(chan struct{})

		for {
			r, _, err := reader.ReadRune()

			if !me {
				acquire <- struct{}{}
				me = true
				go func() {
					<-cancel
				}()
			}

			cancel <- struct{}{}

			if err != nil {
				(&Rune{Type: tpe, Title: id, End: true, IsStderr: isStderr}).Log()
				me = false
				<-acquire
				return
			}

			if r == BR {
				(&Rune{Type: tpe, Title: id, Rune: r, IsStderr: isStderr}).Log()
				me = false
				<-acquire
				continue
			}

			(&Rune{Type: tpe, Title: id, Rune: r, IsStderr: isStderr}).Log()

			go func() {
				select {
				case <-cancel:
				case <-time.After(time.Second / 2):
					me = false
					<-acquire
				}
			}()
		}
	}()

	return ww
}

func run(ctx context.Context, name, command string, env []string) error {
	cmd := exec.Command("sh", "-c", command)

	stdout := pipeChar(TypeCommandOK, name, false)
	cmd.Stdout = stdout
	defer stdout.Close()

	stderr := pipeChar(TypeCommandErr, name, true)
	cmd.Stderr = stderr
	defer stderr.Close()

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
