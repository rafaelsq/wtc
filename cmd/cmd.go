package cmd

import (
	"bufio"
	"context"
	"os/exec"

	"gitlab.com/rafaelsq/wtc/async"
)

type Type uint8

const (
	Default Type = iota
	Error
)

type Msg struct {
	Type Type
	Text string
}

func CMD(ctx context.Context, out chan Msg, command ...string) error {
	cmd := exec.Command(command[0], command[1:]...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		_ = cmd.Process.Kill()
	}()

	err = async.Run(ctx, func(ctx context.Context) error {
		bi := bufio.NewScanner(stdout)
		for {
			if !bi.Scan() {
				break
			}

			out <- Msg{Text: bi.Text()}
		}

		if err := bi.Err(); err != nil {
			return err
		}
		return nil
	}, func(ctx context.Context) error {
		bi := bufio.NewScanner(stderr)
		for {
			if !bi.Scan() {
				break
			}

			out <- Msg{Text: bi.Text(), Type: Error}
		}

		if err := bi.Err(); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	if err := cmd.Wait(); err != nil {
		return err
	}

	return nil
}
