package server

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/nats-io/nats-server/v2/logger"
	"github.com/nats-io/nats-server/v2/server"
)

type NatsServer struct {
	RemoteHostPort string
	Server *server.Server
	Logger *logger.Logger
}

func NewNatsServer(remoteHostPort string, logger *logger.Logger) (*NatsServer, error) {
	var ns NatsServer
	ns.RemoteHostPort = remoteHostPort
	ns.Logger = logger
	if ns.RemoteHostPort == "" {
		// Create a FlagSet and sets the usage
		fs := flag.NewFlagSet("nats-server", flag.ExitOnError)
		args := GetArgs()
		hasConfig := args.indexOf("-c") != -1 || args.indexOf("-config") != -1
		if hasConfig {
			logger.Noticef("ignoring -a, -addr, -p and -port flags since a config was specified")
		}

		if !hasConfig &&  args.indexOf("-a") == -1 && args.indexOf("-addr") == -1 {
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

		// gnatsd will also handle the signal if this is not set
		opts.NoSigs = true

		s := server.New(opts)
		ns.Server = s
	}

	return &ns, nil
}

func (server *NatsServer) Start() error {
	if server.RemoteHostPort == "" {
		server.Server.ConfigureLogger()

		go server.Server.Start()

		if !server.Server.ReadyForConnections(5 * time.Second) {
			return errors.New("unable to start embedded gnatsd server")
		}
	}

	return nil
}

func (server *NatsServer) Shutdown() {
	if server.Server != nil {
		server.Server.Shutdown()
	}
}

func (server *NatsServer) HostPort() string {
	if server.Server != nil {
		return server.Server.Addr().(*net.TCPAddr).String()
	}
	return server.RemoteHostPort
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
