package main

import (
	"context"
	"github.com/kitanovicd/blindspot/internal/cli"
	"os"
	"os/signal"
	"syscall"
)

func main() { os.Exit(run()) }

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
}
