# Watch

A simple watch files utility  
You can use it to watch files and run any command.  
It is not necessary to be a Golang project.  

```bash
$ go get -u github.com/rafaelsq/wtc
$ cd my_go_project
$ wtc "go build main.go" "./my_go_project"
```

You can create an Yaml file with your rules.
Default;
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

If you run `wtc "<build-cmd>" "<run-cmd>"`, it will replace default `command`s above.  
If you create your own `.wtc.yaml` or `wtc.yaml`, no default rules will exists.
