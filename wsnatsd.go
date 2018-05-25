package main

import (
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
}

func NewBridge() (*Bridge, error) {
	var err error
	bridge := Bridge{}
	bridge.Logger = logger.NewStdLogger(true, true, true, true, true)

	bridge.NatsServer, err = server.NewNatsServer()
	if err != nil {
		return nil, err
	}

	bridge.WsServer, err = server.NewWsServer(bridge.Logger)
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
	return path.Join(os.Getenv("TMPDIR"), fmt.Sprintf("wsgnatsd_%d.pid", os.Getpid()))
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
