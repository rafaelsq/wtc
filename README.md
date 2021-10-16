# WTC

[![Actions Status](https://github.com/rafaelsq/wtc/workflows/tests/badge.svg)](https://github.com/rafaelsq/wtc/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/rafaelsq/wtc)](https://goreportcard.com/report/github.com/rafaelsq/wtc)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/rafaelsq/wtc/blob/master/LICENSE)

WTC is a simple utility you can use to watch files and execute commands.  

## Install

From master branch  
`$ go install github.com/rafaelsq/wtc@latest`  

You can also install by release(linux64 only);  
`$ curl -sfL --silent https://github.com/rafaelsq/wtc/releases/latest/download/wtc.linux64.tar.gz | tar -xzv && mv wtc $(go env GOPATH)/bin/`

Or just head to the [releases page](https://github.com/rafaelsq/wtc/releases) and download the latest version for you platform.

## Compile from source

Before you begin, ensure you have installed the latest version of Go. See the [Go documentation](https://golang.org/doc/install) for details.

`$ go get -u github.com/rafaelsq/wtc`  


## Usage

```
$ wtc --help
USAGE:
wtc [[flags] [regex command]]
        e.g.: wtc
            // will read [.]wtc.y[a]ml
        e.g.: wtc "_test\.go$" "go test -cover {PKG}"

wtc [flags]] [rule-name]
        e.g.: wtc -t rule-name
             wtc --no-trace "rule ruleb"
FLAGS:
  -debounce int
        global debounce (default 300)
  -f string
        wtc config file (default try to find [.]wtc.y[a]ml)
  -ignore string
        regex
  -no-trace
        disable messages.
  -t string
        trig one or more rules by name
                e.g.: wtc -t ruleA
                     wtc -t "ruleA ruleB"
```

## Usage with [.]wtc.y[a]ml 

You can configure WTC by creating an YAML file with your own rules.

Example with all options:

```yaml
no_trace: false
debounce: 300  # if rule has no debounce, this will be used instead
ignore: \.git/
format:
  time: "15:04:05" # golang format
  ok: "\u001b[38;5;244m[{{.Time}}] \u001b[38;5;2m[{{.Title}}]\u001b[0m \u001b[38;5;238m{{.Message}}\u001b[0m\n"
  fail: "\u001b[38;5;244m[{{.Time}}] \u001b[38;5;1m[{{.Title}}] \u001b[38;5;238m{{.Message}}\u001b[0m\n"
  command_ok: "\u001b[38;5;240m[{{.Time}}] [{{.Title}}] \u001b[0m{{.Message}}\n"
  command_err: "\u001b[38;5;240m[{{.Time}}] [{{.Title}}] \u001b[38;5;1m{{.Message}}\u001b[0m\n"
trig: [start, buildNRun]  # will run start and after buildNRun
trig_async: # will run test and async concurrently
  - test
  - async
env:
  - name: PORT
    value: 2000
  - type: file
    name: ./base.env
rules:
  - name: start
  - name: buildNRun
    match: \.go$
    ignore: _test\.go$
    command: go build
    env:
      - name: ENV
        value: development
      - name: %{BASE_FILE}% # replace from environment
        type: file
    trig: 
      - done build
      - run
      - test
  - name: done build
  - name: run
    command: ./$(basename `pwd`)
  - name: test
    env:
      - name: ENV
        value: test
      - name: %{BASE_FILE}%
        type: file
    match: _test\.go$
    command: go test -cover {PKG}
  - name: async
    command: echo async
```

Example base.env

```bash
export PORT=3000

# will be replaced by the environment variable
ENVIRONMENT=%{ENV}%
```

You can also trig a rule using `wtc -t`, example;  
`wtc -t "start buildNRun"`  
`wtc --no-trace buildNRun`  


## Dev

`$ make` will watch for changes and run `go install` or just run `$ go run main.go`
```yaml
debounce: 100
ignore: "\\.git/"
trig: install
rules:
  - name: install
    match: "\\.go$"
    command: "go install"
```
