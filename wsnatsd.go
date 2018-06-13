package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path"
	"runtime"
	"strings"
	"syscall"

	"github.com/aricart/wsgnatsd/server"
	"github.com/nats-io/gnatsd/logger"
)

var bridge *Bridge

type Bridge struct {
	Logger     *logger.Logger
	WsServer   *server.WsServer
	NatsServer *server.NatsServer
	PidFile    *os.File
	PidDir     string
}

func NewBridge() (*Bridge, error) {
	var err error
	bridge := Bridge{}
	conf, pidDir, err := bridge.parseOptions()
	if err != nil {
		return nil, err
	}

	bridge.Logger = logger.NewStdLogger(true, conf.Debug, conf.Trace, true, true)

	bridge.PidDir = pidDir
	fi, err := os.Stat(bridge.PidDir)
	if os.IsNotExist(err) {
		bridge.Logger.Fatalf("PidDir [%s] doesn't exist", bridge.PidDir)
	}
	if !fi.IsDir() {
		bridge.Logger.Fatalf("PidDir [%s] is not a directory", bridge.PidDir)
	}
	bridge.NatsServer, err = server.NewNatsServer()
	if err != nil {
		return nil, err
	}

	bridge.WsServer, err = server.NewWsServer(conf, bridge.Logger)
	if err != nil {
		return nil, err
	}

	return &bridge, nil
}

func (b *Bridge) Start() error {
	if err := b.NatsServer.Start(); err != nil {
		return err
	}
	if err := b.WsServer.Start(bridge.NatsServer.HostPort()); err != nil {
		return err
	}

	if err := b.writePidFile(); err != nil {
		return err
	}

	return nil
}

func (b *Bridge) Shutdown() {
	b.WsServer.Shutdown()
	b.NatsServer.Shutdown()
	b.Cleanup()
}

func (b *Bridge) Cleanup() {
	err := b.PidFile.Close()
	if err != nil {
		b.Logger.Errorf("Failed closing pid file: %v", err)
	}
	if err := os.Remove(b.pidPath()); err != nil {
		b.Logger.Errorf("Failed removing pid file: %v", err)
	}
}

func (b *Bridge) pidPath() string {
	return path.Join(b.PidDir, fmt.Sprintf("wsgnatsd_%d.pid", os.Getpid()))
}

func (b *Bridge) writePidFile() error {
	var err error
	b.PidFile, err = os.Create(b.pidPath())
	if err != nil {
		return err
	}
	_, err = b.PidFile.Write([]byte(strings.Join([]string{b.WsServer.GetURL(), b.NatsServer.GetURL()}, "\n")))
	if err != nil {
		return err
	}

	return nil
}

func (b *Bridge) BridgeArgs() []string {
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

func (b *Bridge) usage() {
	usage := "wsgnatsd [-hp localhost:8080] [-cert <certfile>] [-key <keyfile>] [-X] [-pid <piddir>] [-D] [-V] [-DV] [-- <gnatsd opts>]\nIf no gnatsd options are provided the embedded server runs at 127.0.0.1:-1 (auto selected port)"
	fmt.Println(usage)
}

func (b *Bridge) parseOptions() (server.Conf, string, error) {
	opts := flag.NewFlagSet("bridge-conf", flag.ExitOnError)
	opts.Usage = b.usage

	var pidDir string
	var debugAndTrace bool

	conf := server.Conf{}
	opts.StringVar(&conf.HostPort, "hp", "127.0.0.1:0", "http hostport - (default is autoassigned port)")
	opts.StringVar(&conf.CaFile, "ca", "", "tls ca certificate")
	opts.StringVar(&conf.CertFile, "cert", "", "tls certificate")
	opts.StringVar(&conf.KeyFile, "key", "", "tls key")
	opts.StringVar(&pidDir, "pid", os.Getenv("TMPDIR"), "pid path")
	opts.BoolVar(&conf.Text, "text", false, "use text websocket frames")
	opts.BoolVar(&conf.Debug, "D", false, "enable debugging output")
	opts.BoolVar(&conf.Trace, "V", false, "enable tracing output")
	opts.BoolVar(&debugAndTrace, "DV", false, "Debug and trace")

	if debugAndTrace {
		conf.Trace = true
		conf.Debug = true
	}

	if err := opts.Parse(b.BridgeArgs()); err != nil {
		b.usage()
		os.Exit(0)
	}

	if conf.KeyFile != "" || conf.CertFile != "" {
		if conf.KeyFile == "" || conf.CertFile == "" {
			b.Logger.Fatalf("if -cert or -key is specified, both must be supplied")
		}
	}

	return conf, pidDir, nil
}

func main() {
	var err error
	bridge, err = NewBridge()
	if err != nil {
		bridge.Logger.Errorf("Failed to create sub-systems: %v\n")
		panic(err)
	}

	if err := bridge.Start(); err != nil {
		bridge.Logger.Errorf("Failed to start sub-systems: %v\n")
		bridge.Shutdown()
		panic(err)
	}

	handleSignals()

	if bridge != nil {
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
				bridge.Shutdown()
				os.Exit(0)
			}
		}
	}()
}
