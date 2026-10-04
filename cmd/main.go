package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/benbenbang/ts-jev-go-sdk/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, in io.Reader, out, stderr io.Writer) int {
	cmd := cli.NewRoot()
	cmd.SetArgs(args)
	cmd.SetIn(in)
	cmd.SetOut(out)
	cmd.SetErr(stderr)
	if err := cmd.ExecuteContext(ctx); err != nil {
		logger := cli.Logger(cmd)
		logger.Error().Err(err).Msg("command failed")
		return 1
	}
	return 0
}
