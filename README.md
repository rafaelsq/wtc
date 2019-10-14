# Watch

[![Actions Status](https://github.com/rafaelsq/wtc/workflows/tests/badge.svg)](https://github.com/rafaelsq/wtc/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/rafaelsq/wtc)](https://goreportcard.com/report/github.com/rafaelsq/wtc)
[![GoDoc](https://godoc.org/github.com/rafaelsq/wtc?status.svg)](https://godoc.org/github.com/rafaelsq/wtc)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/rafaelsq/wtc/blob/master/LICENSE)

Watch is a simple watch files utility you can use to watch files and run commands.  
Although the utility is written in [Go](https://golang.org/), you can use it for projects written in any programming language.

## Prerequisites

Before you begin, ensure you have installed the latest version of Go. See the [Go documentation](https://golang.org/doc/install) for details.

## Install

`$ go get -u github.com/rafaelsq/wtc`

## Usage

```
USAGE:
$ wtc [[flags] regex command]

If [.]wtc.yaml exists, it will be used.

FLAGS:
  -debounce int
    	global debounce (default 300)
  -ignore string
    	regex
  -no-trace
    	disable messages.
```

### Example

`wtc "_test\.go$" "go test -cover {PKG}"`


## Usage with [.]wtc.yaml 

You can configure Watch by creating an YAML file with your own rules.

Example:

```yaml
no_trace: false
debounce: 300  # if rule has no debounce, this will be used instead
ignore: "\\.git/"
trig: buildNRun  # will run on start
rules:
  - name: buildNRun
    match: "\\.go$"
    ignore: "_test\\.go$"
    command: "go build"
    trig: run
  - name: run
    command: "./$(basename `pwd`)"
  - name: test
    match: "_test\\.go$"
    command: "go test -cover {PKG}"
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
