# WTC

[![Actions Status](https://github.com/rafaelsq/wtc/workflows/tests/badge.svg)](https://github.com/rafaelsq/wtc/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/rafaelsq/wtc)](https://goreportcard.com/report/github.com/rafaelsq/wtc)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/rafaelsq/wtc/blob/master/LICENSE)

WTC is a simple utility you can use to watch files and execute commands.  

## Install

Latest Release  
`$ curl -sfL --silent https://github.com/rafaelsq/wtc/releases/latest/download/wtc.linux64.tar.gz | tar -xzv && mv wtc $(go env GOPATH)/bin/`

## Compile from source

Before you begin, ensure you have installed the latest version of Go. See the [Go documentation](https://golang.org/doc/install) for details.

`$ go get -u github.com/rafaelsq/wtc`

## Usage

```
$ wtc [[flags] [regex command]]

FLAGS:
  -debounce int
        global debounce (default 300)
  -f string
        wtc config file (default try to find [.]wtc.y[a]ml)
  -ignore string
        regex
  -no-trace
        disable messages.
```

### Example

`wtc "_test\.go$" "go test -cover {PKG}"`


## Usage with [.]wtc.yaml 

You can configure WTC by creating an YAML file with your own rules.

Example:

```yaml
no_trace: false
debounce: 300  # if rule has no debounce, this will be used instead
ignore: \.git/
format:
  ok: "{{.Time}} \u001b[38;5;2m[{{.Name}}]\u001b[0m - {{.Command}}\n"
  fail: "{{.Time}} \u001b[38;5;1m[{{.Name}}]\u001b[0m - {{.Error}}\n"
trig: [start, buildNRun]  # will run on start
env:
  - name: PORT
    value: 2000
rules:
  - name: start
  - name: buildNRun
    match: \.go$
    ignore: _test\.go$
    command: go build
    env:
      - name: development
        value: true
    trig: 
      - done build
      - run
      - test
  - name: done build
  - name: run
    command: ./$(basename `pwd`)
  - name: test
    match: _test\.go$
    command: go test -cover {PKG}
```


## Dev

`$ make` will watch for changes and run `go install`
```yaml
debounce: 100
ignore: "\\.git/"
trig: install
rules:
  - name: install
    match: "\\.go$"
    command: "go install"
```
