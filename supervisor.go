package sprout

import (
	"crypto/tls"
	"fmt"
	"log"
	"time"

	"git.sr.ht/~whereswaldon/forest-go/store"
)

// LaunchSupervisedWorker launches a worker in a new goroutine that will
// connect to `addr` and use `store` as its node storage. It will dial
// using the provided `tlsConfig`, and it will log errors on the given
// `logger`.
//
// BUG(whereswaldon): this interface is experimental and likely to change.
func LaunchSupervisedWorker(done <-chan struct{}, addr string, s store.ExtendedStore, tlsConfig *tls.Config, logger *log.Logger) {
	go func() {
		firstAttempt := true
		for {
			if !firstAttempt {
				logger.Printf("Restarting worker for address %s", addr)
				time.Sleep(time.Second)
			}
			firstAttempt = false
			conn, err := tls.Dial("tcp", addr, tlsConfig)
			if err != nil {
				logger.Printf("Failed to connect to %s: %v", addr, err)
				continue
			}
			worker, err := NewWorker(done, conn, s)
			if err != nil {
				logger.Printf("Failed launching worker to connect to address %s: %v", addr, err)
				continue
			}
			worker.Logger = log.New(logger.Writer(), fmt.Sprintf("worker-%v ", addr), log.Flags())
			go worker.BootstrapLocalStore(1024)

			// block until the worker dies
			worker.Run()
			select {
			case <-done:
				return
			default:
			}
		}
	}()
}
