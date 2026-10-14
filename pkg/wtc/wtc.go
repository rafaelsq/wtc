package wtc

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/radovskyb/watcher"
	"gopkg.in/yaml.v2"
)

var (
	appContext              context.Context
	contexts                map[string]context.CancelFunc
	contextsLock            map[string]chan struct{}
	ctxmutex                sync.Mutex
	contextsLockMutex       sync.Mutex
	envFileLastModified     = make(map[string]time.Time)
	envFileLastModifiedLock sync.Mutex
	envFileKeys             = make(map[string]string)
)

var (
	logger chan LogChunk

	exportRe     = regexp.MustCompile(`(?i)export\s+`)
	replaceEnvRe = regexp.MustCompile(`(?i)\%\{[A-Z0-9_]+\}\%`)
	ignoreRuleRe = regexp.MustCompile(`(?i)\s*,\s*`)

	TimeFormat = "15:04:05"

	TypeOK         = "\u001b[38;5;244m[{{.Time}}] \u001b[38;5;2m[{{.Title}}]\u001b[0m \u001b[38;5;238m{{.Message}}\u001b[0m\n"
	TypeFail       = "\u001b[38;5;244m[{{.Time}}] \u001b[38;5;1m[{{.Title}}] \u001b[38;5;238m{{.Message}}\u001b[0m\n"
	TypeCommandOK  = "\u001b[38;5;240m[{{.Time}}] [{{.Title}}] \u001b[0m{{.Message}}\n"
	TypeCommandErr = "\u001b[38;5;240m[{{.Time}}] [{{.Title}}] \u001b[38;5;1m{{.Message}}\u001b[0m\n"

	SimpleTypeOK         = "{{.Message}}\n"
	SimpleTypeFail       = "\u001b[38;5;238m{{.Message}}\u001b[0m\n"
	SimpleTypeCommandOK  = "\u001b[0m{{.Message}}\n"
	SimpleTypeCommandErr = "\u001b[38;5;1m{{.Message}}\u001b[0m\n"
)

// LogChunk represents a stream of bytes, allowing partial lines while maintaining thread safety
type LogChunk struct {
	Title    string
	Text     string
	IsStderr bool
	Tpe      string
	IsEOF    bool
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
				"\te.g.: wtc\n\t    // will read [.]wtc.y[a]ml\n"+
				"\te.g.: wtc -r -sfmt \"_test\\.go$\" \"go test -cover {PKG}\"\n\n"+
				"wtc [flags]] [rule-name]\n\te.g.: wtc -t rule-name\n\t     wtc --no-trace \"rule ruleb\"\n\t"+
				"     wtc -arg-pkg any/path rule\n\t"+
				"     wtc -arg-file path/to/file.ext rule\n"+
				"FLAGS:\n",
		)
		flag.PrintDefaults()
	}

	config := &Config{
		Debounce:    300,
		KillSignal:  syscall.SIGKILL,
		KillTimeout: 3,
	}

	var configFilePath string

	flag.IntVar(&config.Debounce, "debounce", 300, "global debounce")
	flag.StringVar(&config.Ignore, "ignore", "", "regex")
	flag.BoolVar(&config.NoTrace, "no-trace", false, "disable messages.")
	flag.StringVar(&configFilePath, "f", "", "wtc config file (default try to find [.]wtc.y[a]ml)")

	var trigs string
	flag.StringVar(&trigs, "t", "", "trig one or more rules by name\n\te.g.: wtc -t ruleA\n\t     wtc -t \"ruleA ruleB\"")

	var rawIgnoreRules string
	flag.StringVar(&rawIgnoreRules, "ignore-rules", "", "ignore one or more rules (e.g.: -ignore-rules \"ruleA,ruleB\"\n"+
		"                          or export WTC_IGNORE_RULES=ruleA,ruleB)")

	flag.StringVar(&config.PkgArgument, "arg-pkg", "", "wtc -arg-pkg path/to/folder rule-name")
	flag.StringVar(&config.FileArgument, "arg-file", "", "wtc -arg-file path/to/file rule-name")

	var shouldRun bool
	flag.BoolVar(&shouldRun, "r", false, "run on start")

	var simpleFormat bool
	flag.BoolVar(&simpleFormat, "sfmt", false, "simple format(stderr red)")

	flag.Parse()

	if raw := os.Getenv("WTC_IGNORE_RULES"); raw != "" {
		rawIgnoreRules = raw
	}
	if trimmed := strings.TrimSpace(rawIgnoreRules); trimmed != "" {
		config.IgnoreRules = ignoreRuleRe.Split(trimmed, -1)
	}

	ok, err := readConfig(config, configFilePath)
	if err != nil {
		log.Fatal(err)
	}

	if ok {
		if flag.NArg() == 1 {
			trigs = flag.Arg(0)
		}
	} else {
		if flag.NArg() < 2 {
			_, _ = fmt.Fprintf(os.Stderr, "No [.]wtc.yaml or valid command provided.\n")
			flag.CommandLine.Usage()
			os.Exit(2)
		}

		config.Rules = append(config.Rules, &Rule{
			Name:    "run",
			Match:   flag.Arg(0),
			Command: flag.Arg(1),
		})

		if shouldRun {
			config.Trig = []string{"run"}
		}
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

	if simpleFormat {
		TypeOK = SimpleTypeOK
		TypeFail = SimpleTypeFail
		TypeCommandOK = SimpleTypeCommandOK
		TypeCommandErr = SimpleTypeCommandErr
	}

	return config
}

func Start(cfg *Config) {
	var cancelAll context.CancelFunc

	config = cfg
	ctxmutex.Lock()
	appContext, cancelAll = context.WithCancel(context.Background())
	contexts = make(map[string]context.CancelFunc)
	ctxmutex.Unlock()

	contextsLockMutex.Lock()
	contextsLock = make(map[string]chan struct{})
	contextsLockMutex.Unlock()

	dir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	logger = make(chan LogChunk, 500)

	// Centralized terminal formatting and multiplexing goroutine
	go func() {
		var activeTitle string
		var activeTpe string
		var activeIsStderr *bool
		var midLine bool

		printSuffix := func() {
			if !midLine {
				return
			}
			outStr := strings.ReplaceAll(activeTpe, "{{.Time}}", time.Now().Format(TimeFormat))
			outStr = strings.ReplaceAll(outStr, "{{.Title}}", activeTitle)
			parts := strings.Split(outStr, "{{.Message}}")

			suffix := "\n"
			if len(parts) >= 2 {
				suffix = parts[1]
			}
			if !strings.HasSuffix(suffix, "\n") {
				suffix += "\n"
			}

			out := os.Stdout
			if activeIsStderr != nil && *activeIsStderr {
				out = os.Stderr
			}
			fmt.Fprint(out, suffix)
			midLine = false
		}

		for c := range logger {
			if c.IsEOF {
				if c.Title == activeTitle {
					printSuffix()
				}
				continue
			}

			out := os.Stdout
			if c.IsStderr {
				out = os.Stderr
			}

			// If switching processes while mid-line, cleanly break the line first
			if midLine && (activeTitle != c.Title || (activeIsStderr != nil && *activeIsStderr != c.IsStderr)) {
				printSuffix()
			}

			activeTitle = c.Title
			activeTpe = c.Tpe
			isErr := c.IsStderr
			activeIsStderr = &isErr

			lines := strings.Split(c.Text, "\n")
			for i, line := range lines {
				isLast := i == len(lines)-1

				if !midLine {
					// Skip printing prefix if this is just an empty trailing string from a newline split
					if isLast && line == "" {
						continue
					}

					outStr := strings.ReplaceAll(activeTpe, "{{.Time}}", time.Now().Format(TimeFormat))
					outStr = strings.ReplaceAll(outStr, "{{.Title}}", activeTitle)
					parts := strings.Split(outStr, "{{.Message}}")
					fmt.Fprint(out, parts[0])
					midLine = true
				}

				fmt.Fprint(out, line)

				// If there are more elements in the split, it means a '\n' occurred in the chunk
				if !isLast {
					printSuffix() // prints the format reset and the newline
				}
			}
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
				log.Println(err)
			case <-w.Closed:
				return
			case <-exitSignal:
				cancelAll()
				for _, r := range config.Rules {
					contextsLockMutex.Lock()
					l, exists := contextsLock[r.Name]
					contextsLockMutex.Unlock()
					if exists {
						select {
						case l <- struct{}{}:
							<-l
						default:
						}
					}
				}
				w.Close()
			case e := <-w.Event:
				if e.IsDir() {
					continue
				}

				path := e.Path
				pieces := strings.Split("."+strings.Split(path, dir)[1], "/")
				pkg := strings.Join(pieces[:len(pieces)-1], "/")

				if config.Ignore != "" && retrieveRegexp(config.Ignore).MatchString(path) {
					continue
				}

				for _, rule := range config.Rules {
					rule := rule
					if rule.Ignore != "" && retrieveRegexp(rule.Ignore).MatchString(path) {
						continue
					}

					if rule.Match != "" && retrieveRegexp(rule.Match).MatchString(path) {
						if strIn(rule.Name, config.IgnoreRules) {
							continue
						}

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

	<-exit
}

func findFile() ([]byte, error) {
	for _, file := range []string{"wtc.yaml", ".wtc.yaml", "wtc.yml", ".wtc.yml"} {
		if _, err := os.Stat(file); err == nil {
			return os.ReadFile(file)
		}
	}
	return nil, nil
}

func readConfig(config *Config, filePath string) (bool, error) {
	var yamlFile []byte
	var err error
	if len(filePath) != 0 {
		yamlFile, err = os.ReadFile(filePath)
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
			pieces := strings.SplitN(v, "=", 2)
			if len(pieces) == 2 {
				keys[pieces[0]] = pieces[1]
			}
		}

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
	defer regexpMutex.Unlock()
	result, ok := regexpMap[pattern]
	if !ok {
		result = regexp.MustCompile(pattern)
		regexpMap[pattern] = result
	}
	return result
}

// Log handles manual system messages, ensuring they break their lines cleanly.
func Log(name, tpe, msg string, isStderr bool) {
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}
	logger <- LogChunk{
		Title:    name,
		Text:     msg,
		IsStderr: isStderr,
		Tpe:      tpe,
	}
}

func strIn(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func findAndTrig(async bool, key []string, pkg, path string) {
	var wg sync.WaitGroup
	for _, s := range key {
		if strIn(s, config.IgnoreRules) {
			continue
		}

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

	contextsLockMutex.Lock()
	queue, has := contextsLock[rule.Name]
	if !has {
		queue = make(chan struct{}, 1)
		contextsLock[rule.Name] = queue
	}
	contextsLockMutex.Unlock()

	queue <- struct{}{}
	defer func() { <-queue }()
	defer cancel()

	debounce := config.Debounce
	if rule.Debounce != nil {
		debounce = *rule.Debounce
	}

	select {
	case <-ctx.Done():
		return nil
	case <-time.After(time.Duration(debounce) * time.Millisecond):
	}

	if config.PkgArgument != "" {
		pkg = config.PkgArgument
	}
	if config.FileArgument != "" {
		path = config.FileArgument
	}

	cmd := strings.ReplaceAll(strings.ReplaceAll(rule.Command, "{PKG}", pkg), "{FILE}", path)

	keys := map[string]string{}
	envs := os.Environ()
	for _, v := range envs {
		pieces := strings.SplitN(v, "=", 2)
		if len(pieces) == 2 {
			keys[pieces[0]] = pieces[1]
		}
	}
	for _, e := range append(config.Env, rule.Env...) {
		if strings.ToLower(e.Type) == "file" {
			if e.Name == "" {
				Log(rule.Name, TypeFail, "field \"name\" must point to a file", true)
				return fmt.Errorf("field \"name\" must point to a file")
			}

			fileInfo, err := os.Stat(e.Name)
			if err != nil {
				Log(rule.Name, TypeFail, err.Error(), true)
				return err
			}

			envFileLastModifiedLock.Lock()
			if lastModified, ok := envFileLastModified[e.Name]; !ok || fileInfo.ModTime().Sub(lastModified) > 0 {
				b, err := os.ReadFile(e.Name)
				if err != nil {
					envFileLastModifiedLock.Unlock()
					Log(rule.Name, TypeFail, err.Error(), true)
					return err
				}

				envFileKeys[e.Name] = string(b)
				envFileLastModified[e.Name] = fileInfo.ModTime()
			}
			body := envFileKeys[e.Name]
			envFileLastModifiedLock.Unlock()

			body = replaceEnvRe.ReplaceAllStringFunc(body, func(k string) string {
				return keys[strings.TrimSuffix(strings.TrimPrefix(k, "%{"), "}%")]
			})

			for _, l := range strings.Split(body, "\n") {
				l := strings.TrimSpace(l)
				if len(l) > 0 && l[0] != '#' {
					pieces := strings.SplitN(exportRe.ReplaceAllString(l, ""), "=", 2)
					if len(pieces) == 2 {
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
		return nil
	}
	if err != nil {
		return err
	}

	if len(rule.TrigAsync) > 0 {
		go findAndTrig(true, rule.TrigAsync, pkg, path)
	}
	if len(rule.Trig) > 0 {
		findAndTrig(false, rule.Trig, pkg, path)
	}

	return nil
}

// pipeStream reads chunks of bytes instead of lines, allowing real-time prompts and progress bars
func pipeStream(r io.Reader, tpe, id string, isStderr bool) {
	buf := make([]byte, 1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			logger <- LogChunk{
				Title:    id,
				Text:     string(buf[:n]),
				IsStderr: isStderr,
				Tpe:      tpe,
			}
		}
		if err != nil {
			logger <- LogChunk{Title: id, IsEOF: true}
			break
		}
	}
}

func killProcessAndChildren(cmd *exec.Cmd, done <-chan struct{}) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	if runtime.GOOS == "windows" {
		_ = cmd.Process.Kill()
		return
	}

	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Setsid {
		_ = syscall.Kill(-cmd.Process.Pid, config.KillSignal)

		if config.KillSignal != syscall.SIGKILL {
			go func() {
				t := time.NewTimer(time.Duration(config.KillTimeout) * time.Second)
				defer t.Stop()

				select {
				case <-t.C:
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				case <-done:
				}
			}()
		}
	} else {
		_ = cmd.Process.Kill()
	}
}

func run(ctx context.Context, name, command string, env []string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}

	var stdout, stderr io.ReadCloser
	var err error

	var tty, tty2 *os.File

	if runtime.GOOS == "windows" {
		stdout, err = cmd.StdoutPipe()
		if err != nil {
			return err
		}
		stderr, err = cmd.StderrPipe()
		if err != nil {
			return err
		}
	} else {
		stdout, tty, err = pty.Open()
		if err != nil {
			return err
		}
		defer stdout.Close()

		stderr, tty2, err = pty.Open()
		if err != nil {
			tty.Close()
			return err
		}
		defer stderr.Close()

		cmd.Stdin = os.Stdin
		cmd.Stdout = tty
		cmd.Stderr = tty2
	}

	cmd.Env = env

	if err := cmd.Start(); err != nil {
		if tty != nil {
			tty.Close()
		}
		if tty2 != nil {
			tty2.Close()
		}
		return err
	}

	if tty != nil {
		tty.Close()
	}
	if tty2 != nil {
		tty2.Close()
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			killProcessAndChildren(cmd, done)
		case <-done:
		}
	}()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		pipeStream(stdout, TypeCommandOK, name, false)
	}()
	go func() {
		defer wg.Done()
		pipeStream(stderr, TypeCommandErr, name, true)
	}()

	err = cmd.Wait()
	close(done)

	wg.Wait()

	if err != nil && cmd.ProcessState != nil && runtime.GOOS != "windows" {
		if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
			if ws.Signaled() && ws.Signal() == syscall.SIGKILL {
				err = context.Canceled
			}
		}
	}

	return err
}
