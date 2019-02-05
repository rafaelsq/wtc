package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/rjeczalik/notify"

	"gitlab.com/rafaelsq/wtc/async"
	"gitlab.com/rafaelsq/wtc/cmd"
)

var argCMD = "server"

func buildnrun(ctx context.Context) {
	out := make(chan cmd.Msg)
	done := make(chan struct{}, 1)

	go func() {
		for {
			select {
			case m := <-out:
				if m.Type == cmd.Error {
					fmt.Printf("\x1b[31;1m%s\x1b[0m\n", m.Text)
				} else {
					fmt.Printf("\x1b[32;1m%s\x1b[0m\n", m.Text)
				}
			case <-done:
				return
			}
		}
	}()

	go func() {
		err := async.Run(ctx, func(c context.Context) error {
			err := cmd.CMD(c, out, "go", "build", "-i", "-o", "app")
			if err != nil {
				return err
			}

			return cmd.CMD(c, out, "./app", argCMD)
		})
		if err != nil {
			fmt.Printf("\x1b[31;1merror: %s\x1b[0m\n", err.Error())
		}
		done <- struct{}{}
	}()
}

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

	go func() {
		c, cc := context.WithCancel(ctx)
		cancel = &cc

		buildnrun(c)
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
					buildnrun(c)
				case <-c.Done():
				}
			}()
		}
	}
}
