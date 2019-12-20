package server

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

type NatsServer struct {
	*Opts
	server *server.Server
}

func NewNatsServer(o *Opts) (*NatsServer, error) {
	var ns NatsServer
	ns.Opts = o
	if ns.RemoteNatsHostPort == "" {
		// Create a FlagSet and sets the usage
		fs := flag.NewFlagSet("nats-server", flag.ExitOnError)
		args := GetArgs()
		hasConfig := args.indexOf("-c") != -1 || args.indexOf("-config") != -1
		if hasConfig {
			ns.Logger.Noticef("ignoring -a, -addr, -p and -port flags since a config was specified")
		}

		if !hasConfig && args.indexOf("-a") == -1 && args.indexOf("-addr") == -1 {
			args = append(args, "-a", "127.0.0.1")
		}
		if !hasConfig && args.indexOf("-p") == -1 {
			args = append(args, "-p", "-1")
		}

		opts, err := server.ConfigureOptions(fs, args,
			server.PrintServerAndExit,
			fs.Usage,
			server.PrintTLSHelpAndDie)

		if err != nil {
			server.PrintAndDie(err.Error() + "\n" + usageString)
		}

		if opts == nil {
			// server was given a help arg
			return nil, nil
		}

		// gnatsd will also handle the signal if this is not set
		opts.NoSigs = true

		s, err := server.NewServer(opts)
		if err != nil {
			return nil, err
		}
		ns.server = s
	}

	return &ns, nil
}

func (server *NatsServer) Start() error {
	if server.RemoteNatsHostPort == "" {
		server.server.ConfigureLogger()
		go server.server.Start()
		if !server.server.ReadyForConnections(5 * time.Second) {
			return errors.New("unable to start embedded gnatsd server")
		}
	}
	server.NatsHostPort = server.HostPort()
	return nil
}

func (server *NatsServer) Shutdown() {
	if server.server != nil {
		server.server.Shutdown()
	}
}

func (server *NatsServer) HostPort() string {
	if server.server != nil {
		return server.server.Addr().(*net.TCPAddr).String()
	}
	return server.RemoteNatsHostPort
}

func (server *NatsServer) GetURL() string {
	return fmt.Sprintf("nats://%s", server.HostPort())
}

var usageString = "embedded NATS server options can be supplied by following a '--' argument with any supported flag."

type Args []string

func GetArgs() Args {
	var args Args
	for i, a := range os.Args {
		if a == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	return args
}

func (a Args) getFlag(v string) string {
	idx := a.indexOf(v)
	if idx != -1 && len(a) >= idx+1 {
		return a[idx+1]
	}
	return ""
}

func (a Args) indexOf(v string) int {
	for i, e := range a {
		if v == e {
			return i
		}
	}
	return -1
}
