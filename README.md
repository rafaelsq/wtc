# Watch

[![Actions Status](https://github.com/rafaelsq/wtc/workflows/tests/badge.svg)](https://github.com/rafaelsq/wtc/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/rafaelsq/wtc)](https://goreportcard.com/report/github.com/rafaelsq/wtc)
[![GoDoc](https://godoc.org/github.com/rafaelsq/wtc?status.svg)](https://godoc.org/github.com/rafaelsq/wtc)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/rafaelsq/wtc/blob/master/LICENSE)

Watch is a simple watch files utility you can use to watch files and run commands.  
Although the utility is written in [Go](https://golang.org/), you can use it for projects written in any programming language.

## Prerequisites

Before you begin, ensure you have installed the latest version of Go. See the [Go documentation](https://golang.org/doc/install) for details.

## How to install

You can install Watch as follows:

1. Install the Watch directory:
   
   ```bash
   $ go get -u github.com/rafaelsq/wtc
   ```

2. Change directory to the project where you want to run Watch:
  
  ```bash
  $ cd my_go_project
  ```
  
3. Run the following to build the utility:

  ```bash
  $ wtc -build "go build main.go" -run "./my_go_project"
  ```

## How to use

You can configure Watch by creating an YAML file with your own rules.

The default is:

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

> **_Note:_** If you run `wtc -build "<build-cmd>" -run "<run-cmd>"`, the utility replaces the default commands above.  

If you create your own `.wtc.yaml` or `wtc.yaml`, no default rules will exist.
