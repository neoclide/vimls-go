package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/neoclide/vimls-go/internal/server"
)

func main() {
	os.Exit(run())
}

func run() int {
	version := flag.Bool("version", false, "print version and exit")
	listen := flag.String("listen", "", "listen on a TCP address instead of stdio (for example 127.0.0.1:4389)")
	flag.Parse()
	if *version {
		fmt.Printf("%s %s\n", server.Name, server.Version)
		return 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *listen != "" {
		return runTCP(ctx, *listen)
	}
	return server.New(os.Stdin, os.Stdout, os.Stderr).Run(ctx)
}

func runTCP(ctx context.Context, address string) int {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vimls: listen: %v\n", err)
		return 1
	}
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
		case <-stop:
		}
	}()
	fmt.Fprintf(os.Stderr, "vimls: listening on tcp://%s\n", listener.Addr())
	connection, err := listener.Accept()
	close(stop)
	_ = listener.Close()
	if err != nil {
		if ctx.Err() != nil {
			return 0
		}
		fmt.Fprintf(os.Stderr, "vimls: accept: %v\n", err)
		return 1
	}
	defer connection.Close()
	return server.New(connection, connection, os.Stderr).Run(ctx)
}
