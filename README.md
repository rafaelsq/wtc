# Watch

[![Actions Status](https://github.com/rafaelsq/wtc/workflows/tests/badge.svg)](https://github.com/rafaelsq/wtc/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/rafaelsq/wtc)](https://goreportcard.com/report/github.com/rafaelsq/wtc)
[![GoDoc](https://godoc.org/github.com/rafaelsq/wtc?status.svg)](https://godoc.org/github.com/rafaelsq/wtc)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](https://opensource.org/licenses/MIT)

A simple watch files utility  
You can use it to watch files and run any command.  
It is not necessary to be a Golang project.  

## Installation

```bash
$ go get -u github.com/rafaelsq/wtc
```

## Usage

### Quickstart

If you run `wtc [flags] "<build-cmd>" "<run-cmd>"`, it will replace default `command`s above. 

#### Example

```
$ cd my_go_project
$ wtc "go build main.go" "./my_go_project"
```

### Using a configuration file

You can create an Yaml file with your rules.
If you create your own `.wtc.yaml` or `wtc.yaml`, no default rules will exists.

Default:

```yaml
no_trace: false
debounce: 300
ignore: "\\.git/"
trig: build
rules:
  - name: build
    match: ".go$"
    ignore: "_test\\.go$"
    command: "go build"
    trig: run
  - name: run
    match: "^$"
    command: "./$(basename `pwd`)"
  - name: test
    match: "_test\\.go$"
    command: "go test -cover {PKG}"
```

