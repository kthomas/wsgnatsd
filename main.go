package main

import (
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	selfsignedcert "github.com/kthomas/go-self-signed-cert"
	"github.com/kthomas/nats-server/v2/logger"
	"github.com/kthomas/wsgnatsd/wsproxy"
)

const bridgeTypeEmbedded = "embedded"
const bridgeTypeRemote = "remote"

var bridge *Bridge

type Bridge struct {
	*wsproxy.Opts
	Type       string
	NatsServer *wsproxy.NatsServer
	WsServer   *wsproxy.WsServer
}

func NewBridge(opts *wsproxy.Opts) (*Bridge, error) {
	var err error

	var natsServer *wsproxy.NatsServer
	var wsServer *wsproxy.WsServer
	var bridgeType string

	if opts.RemoteNatsHostPort == "" {
		bridgeType = bridgeTypeEmbedded
	} else {
		bridgeType = bridgeTypeRemote
	}

	natsServer, err = wsproxy.NewNatsServer(opts)
	if err != nil {
		return nil, err
	}

	wsServer, err = wsproxy.NewWsServer(opts)
	if err != nil {
		return nil, err
	}

	return &Bridge{
		opts,
		bridgeType,
		natsServer,
		wsServer,
	}, nil
}

func (b *Bridge) Start() error {
	if b.Type == bridgeTypeEmbedded {
		if err := b.NatsServer.Start(); err != nil {
			return err
		}
	}

	b.WsServer.NatsHostPort = b.NatsServer.HostPort()
	bridge.Logger.Noticef("Bridge using %s NATS server %s", b.Type, b.NatsServer.HostPort())

	if err := b.WsServer.Start(); err != nil {
		return err
	}

	return nil
}

func (b *Bridge) Shutdown() {
	b.WsServer.Shutdown()
	b.NatsServer.Shutdown()
}

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

func parseFlags() (*wsproxy.Opts, error) {
	opts := flag.NewFlagSet("bridge-c", flag.ExitOnError)

	var confFile string
	c := wsproxy.DefaultOpts()
	opts.StringVar(&confFile, "c", "", "configuration file")
	opts.StringVar(&c.WSHostPort, "h", c.WSHostPort, "ws-host - default is 127.0.0.1:4221")
	opts.BoolVar(&c.WSRequireAuthorization, "a", c.WSRequireAuthorization, "ws-require-authorization - when true, the authorization http header provided to the websocket request in the form `bearer: <jwt>` is implicitly used to send a CONNECT message to NATS")
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
		fc, err := wsproxy.LoadOpts(confFile)
		if err != nil {
			return nil, err
		}
		c = *fc
	}

	if c.KeyFile != "" || c.CertFile != "" {
		if c.KeyFile == "" || c.CertFile == "" {
			panic("if -cert or -key is specified, both must be supplied")
		}
	} else {
		privKeyPath, certPath, err := selfsignedcert.GenerateToDisk([]string{})
		if err != nil {
			log.Panicf("failed to generate self-signed certificate; %s", err.Error())
		}
		c.KeyFile = *privKeyPath
		c.CertFile = *certPath
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
