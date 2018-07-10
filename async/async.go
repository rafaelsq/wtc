package async

import (
	"context"
	"fmt"
	"sync"
)

type TypeFunc func(chan bool) error

func Run(parent context.Context, fns ...func(ctx context.Context) error) error {
	cerr := make(chan error, 1)
	ctx, cancelFn := context.WithCancel(parent)

	go func() {
		var wg sync.WaitGroup
		wg.Add(len(fns))

		for _, fn := range fns {
			go func(fn func(ctx context.Context) error) {
				defer wg.Done()
				defer func() {
					if err := recover(); err != nil {
						cancelFn()
						cerr <- fmt.Errorf("async.Run: panic %v", err)
					}
				}()

				if err := fn(ctx); err != nil {
					cancelFn()
					cerr <- err
				}
			}(fn)
		}

		wg.Wait()
		cerr <- nil
	}()

	return <-cerr
}
