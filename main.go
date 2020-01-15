package main

import (
	"errors"
	"flag"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/kthomas/nats-server/v2/logger"
	"github.com/kthomas/wsgnatsd/server"
)

var bridge *Bridge

// var assetServer *server.AssetsServer

type Bridge struct {
	*server.Opts
	WsServer   *server.WsServer
	NatsServer *server.NatsServer
	// PidFile    *os.File
}

func NewBridge(o *server.Opts) (*Bridge, error) {
	var err error
	var bridge Bridge
	bridge.Opts = o

	// fi, err := os.Stat(bridge.PidDir)
	// if os.IsNotExist(err) {
	// 	bridge.Logger.Fatalf("piddir [%s] doesn't exist", bridge.PidDir)
	// }
	// if !fi.IsDir() {
	// 	bridge.Logger.Fatalf("piddir [%s] is not a directory", bridge.PidDir)
	// }

	bridge.NatsServer, err = server.NewNatsServer(o)
	if err != nil {
		return nil, err
	}
	if bridge.NatsServer == nil {
		return nil, nil
	}

	bridge.WsServer, err = server.NewWsServer(o)
	if err != nil {
		return nil, err
	}

	return &bridge, nil
}

func (b *Bridge) Start() error {
	if err := b.NatsServer.Start(); err != nil {
		return err
	}
	kind := "embedded"
	if b.NatsServer == nil {
		kind = "remote"
	}
	bridge.Logger.Noticef("Bridge using %s NATS server %s", kind, b.NatsServer.HostPort())

	if err := b.WsServer.Start(); err != nil {
		return err
	}

	// if err := b.writePidFile(); err != nil {
	// 	return err
	// }

	return nil
}

func (b *Bridge) Shutdown() {
	b.WsServer.Shutdown()
	b.NatsServer.Shutdown()
	// b.Cleanup()
}

func (b *Bridge) Cleanup() {
	// err := b.PidFile.Close()
	// if err != nil {
	// 	b.Logger.Errorf("Failed closing pid file: %v", err)
	// }
	// if err := os.Remove(b.pidPath()); err != nil {
	// 	b.Logger.Errorf("Failed removing pid file: %v", err)
	// }
}

// func (b *Bridge) pidPath() string {
// 	return filepath.Join(b.PidDir, fmt.Sprintf("wsgnatsd_%d.pid", os.Getpid()))
// }

// func (b *Bridge) writePidFile() error {
// 	var err error
// 	b.PidFile, err = os.Create(b.pidPath())
// 	if err != nil {
// 		return err
// 	}
// 	_, err = b.PidFile.Write([]byte(strings.Join([]string{b.WsServer.GetURL(), b.NatsServer.GetURL()}, "\n")))
// 	if err != nil {
// 		return err
// 	}

// 	return nil
// }

func BridgeArgs() []string {
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

func parseFlags() (*server.Opts, error) {
	opts := flag.NewFlagSet("bridge-c", flag.ExitOnError)

	var confFile string
	c := server.DefaultOpts()
	opts.StringVar(&confFile, "c", "", "configuration file")
	opts.StringVar(&c.WSHostPort, "h", c.WSHostPort, "ws-host - default is 127.0.0.1:4219")
	opts.StringVar(&c.RemoteNatsHostPort, "nh", c.RemoteNatsHostPort, "nats-host - disables embedded NATS and proxies requests to the specified hostport")
	opts.StringVar(&c.CaFile, "ca", c.CaFile, "cafile - ca certificate file for ws server")
	opts.StringVar(&c.CertFile, "cert", c.CertFile, "certfile - certificate file for ws server")
	opts.StringVar(&c.KeyFile, "key", c.KeyFile, "keyfile - certificate key for ws server")
	opts.BoolVar(&c.TextFrames, "text", c.TextFrames, "textframes - use text websocket frames")
	opts.BoolVar(&c.Debug, "D", c.Debug, "debug - enable debugging output")
	opts.BoolVar(&c.Trace, "V", c.Trace, "trace - enable tracing output")

	a := BridgeArgs()
	if err := opts.Parse(a); err != nil {
		opts.Usage()
		os.Exit(0)
	}

	if confFile != "" && len(a) > 0 {
		return nil, errors.New("no additional flags for the bridge can be specified in the command")
	}

	if confFile != "" {
		fc, err := server.LoadOpts(confFile)
		if err != nil {
			return nil, err
		}
		c = *fc
	}

	if c.KeyFile != "" || c.CertFile != "" {
		if c.KeyFile == "" || c.CertFile == "" {
			panic("if -cert or -key is specified, both must be supplied")
		}
	}

	return &c, nil
}

func main() {
	o, err := parseFlags()
	if err != nil {
		panic(err)
	}
	o.Logger = logger.NewStdLogger(true, o.Debug, o.Trace, true, true)

	bridge, err = NewBridge(o)
	if err != nil {
		o.Logger.Errorf("Failed to create sub-systems: %v\n")
		panic(err)
	}

	if bridge == nil {
		// prob help option
		return
	}

	if err := bridge.Start(); err != nil {
		o.Logger.Errorf("Failed to start sub-systems: %v\n")
		bridge.Shutdown()
		panic(err)
	}

	// if o.Port > 0 {
	// 	as, err := server.NewAssetsServer(o)
	// 	if err != nil {
	// 		panic(err)
	// 	}
	// 	if err := as.Start(); err != nil {
	// 		panic(err)
	// 	}
	// 	assetServer = as
	// }

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
				// if assetServer != nil {
				// 	assetServer.Shutdown()
				// }
				bridge.Shutdown()
				os.Exit(0)
			}
		}
	}()
}
