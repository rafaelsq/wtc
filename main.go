package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/rjeczalik/notify"

	"gitlab.com/rafaelsq/wtc/cmd"
)

var argCMD = "server"

func main() {
	if len(os.Args) > 1 {
		argCMD = strings.Join(os.Args[1:], " ")
	}

	Watch()
}

func Watch() error {
	c := make(chan notify.EventInfo)

	if err := notify.Watch("./...", c, notify.Create, notify.Write, notify.Remove); err != nil {
		log.Fatal(err)
	}
	defer notify.Stop(c)

	ctx := context.Background()
	var cancel *context.CancelFunc

	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	out := make(chan cmd.Msg)
	build := make(chan context.Context)

	go func() {
		build <- ctx
	}()

	for {
		select {
		case ei := <-c:
			filename := strings.Replace(ei.Path(), dir+"/", "", 1)
			if !strings.HasSuffix(filename, ".go") {
				continue
			}

			if cancel != nil {
				(*cancel)()
			}

			go func() {
				c, cc := context.WithCancel(ctx)
				cancel = &cc

				select {
				case <-time.After(time.Duration(500) * time.Millisecond):
					out <- cmd.Msg{Text: fmt.Sprintf("\n[%v] %s", time.Now().Format("15:04:05"), filename)}
					build <- c
				case <-c.Done():
				}
			}()
		case m := <-out:
			if m.Type == cmd.Error {
				fmt.Printf("\x1b[31;1m%s\x1b[0m\n", m.Text)
			} else {
				fmt.Printf("\x1b[32;1m%s\x1b[0m\n", m.Text)
			}
		case c := <-build:
			go func() {
				if err := cmd.CMD(c, out, "go", "build", "-i", "-o", "app"); err != nil {
					out <- cmd.Msg{
						Text: err.Error(),
						Type: cmd.Error,
					}
					return
				}

				if err := cmd.CMD(c, out, "./app", argCMD); err != nil {
					out <- cmd.Msg{
						Text: fmt.Sprintf("\x1b[31;1merror: %s\x1b[0m\n", err.Error()),
						Type: cmd.Error,
					}
				}
			}()
		}
	}
}
