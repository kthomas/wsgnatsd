package main

import (
	"flag"
	"fmt"
	"github.com/aricart/wsgnatsd/server"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
)

var proxy *server.Server

func usage() {
	usage := `wsgnatsd [-hp localhost:8080] [-cert <certfile>] [-key <keyfile>] [-- <gnatsd opts>]\n`
	fmt.Println(usage)
}

func ParseArgs(args []string) *server.Server {
	opts := flag.NewFlagSet("ws-server", flag.ExitOnError)
	opts.Usage = usage

	config := server.Server{}

	opts.StringVar(&config.HostPort, "hp", "localhost:8080", "http hostport")
	opts.StringVar(&config.CaFile, "ca", "", "tls ca certificate")
	opts.StringVar(&config.CertFile, "cert", "", "tls certificate")
	opts.StringVar(&config.KeyFile, "key", "", "tls key")

	if err := opts.Parse(args); err != nil {
		usage()
		os.Exit(0)
	}

	if config.KeyFile != "" || config.CertFile != "" {
		if config.KeyFile == "" || config.CertFile == "" {
			log.Fatal("When -cert or -key is specified, both must be supplied")
		}
	}

	fmt.Printf("%v\n", config)

	return &config
}

func WsProcessArgs() []string {
	inlineArgs := -1
	for i, a := range os.Args {
		if a == "--" {
			inlineArgs = i
			break
		}
	}

	var args []string
	if inlineArgs == -1 {
		args = os.Args[1:]
	} else {
		args = os.Args[1 : inlineArgs+1]
	}

	return args
}

func SubProcessArgs() []string {
	inlineArgs := -1
	for i, a := range os.Args {
		if a == "--" {
			inlineArgs = i
			break
		}
	}

	var args []string
	if inlineArgs != -1 {
		args = os.Args[inlineArgs+1:]
	}

	return args
}

func main() {
	proxy = ParseArgs(WsProcessArgs())
	fmt.Printf("%v\n", proxy)
	go proxy.Start(SubProcessArgs())

	handleSignals()

	if proxy != nil {
		runtime.Goexit()
	}
}

// handle signals so we can orderly shutdown
func handleSignals() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT)
	go func() {
		for sig := range c {
			switch sig {
			case syscall.SIGINT:
				proxy.Shutdown()
				os.Exit(0)
			}
		}
	}()
}
