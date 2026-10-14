// Command scp is the composition root and the command line of Source Code Portal (architecture 4.5, design 16).
// main contains no logic: it builds the signal context and the process environment and returns run's exit code.
package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// A second signal must kill the process: restore the default handlers as soon as the first one arrived.
	context.AfterFunc(ctx, stop)
	// SIGHUP is NOT the default action of the Go runtime (which is to exit at once, without the shutdown plan): it is a reload request
	// (GitHub App key rotation, pages reload; 05 sections 2.5 and 4.5, design 10 pages.Reload). One slot, so a burst of signals is one reload.
	hup := make(chan struct{}, 1)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGHUP)
	go func() {
		for range sigs {
			select {
			case hup <- struct{}{}:
			default:
			}
		}
	}()
	code := run(ctx, Env{
		Reload:   hup,
		Args:     os.Args[1:],
		Environ:  os.Environ(),
		Hostname: hostname(),
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Listen: func(addr string) (net.Listener, error) {
			return (&net.ListenConfig{}).Listen(context.Background(), "tcp", addr)
		},
		Wire: productWire,
	})
	stop()
	os.Exit(code)
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}
