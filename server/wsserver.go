package server

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/gorilla/websocket"
	"github.com/nats-cloud/nats-service-site/slog"
	"github.com/nats-io/gnatsd/logger"
	"github.com/nats-io/gnatsd/util"
)

type WsServer struct {
	httpServer   *http.Server
	listener     net.Listener
	HostPort     string
	CertFile     string
	KeyFile      string
	CaFile       string
	natsHostPort string
	Logger       *logger.Logger
}

func NewWsServer(Logger *logger.Logger) (*WsServer, error) {
	opts := flag.NewFlagSet("ws-server", flag.ExitOnError)
	opts.Usage = usage

	server := WsServer{}
	server.Logger = Logger

	opts.StringVar(&server.HostPort, "hp", "127.0.0.1:0", "http hostport - (default is autoassigned port)")
	opts.StringVar(&server.CaFile, "ca", "", "tls ca certificate")
	opts.StringVar(&server.CertFile, "cert", "", "tls certificate")
	opts.StringVar(&server.KeyFile, "key", "", "tls key")

	if err := opts.Parse(server.WsProcessArgs()); err != nil {
		usage()
		os.Exit(0)
	}

	if server.KeyFile != "" || server.CertFile != "" {
		if server.KeyFile == "" || server.CertFile == "" {
			return nil, errors.New("if -cert or -key is specified, both must be supplied")
		}
	}

	return &server, nil
}

func (ws *WsServer) Shutdown() {
	if ws.httpServer != nil {
		ws.httpServer.Shutdown(nil)
	}
}

func (ws *WsServer) WsProcessArgs() []string {
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

func usage() {
	usage := "wsgnatsd [-hp localhost:8080] [-cert <certfile>] [-key <keyfile>] [-- <gnatsd opts>]\nIf no gnatsd options are provided the embedded server runs at 127.0.0.1:-1 (auto selected port)"
	fmt.Println(usage)
}

func (ws *WsServer) isTLS() bool {
	return ws.CertFile != ""
}

func parseHostPort(hostPort string) (string, int, error) {
	var err error
	var host, sport string
	var port int
	host, sport, err = net.SplitHostPort(hostPort)
	p, err := strconv.Atoi(sport)
	if err != nil {
		return "", 0, err
	}
	port = p
	return host, port, nil
}

func (ws *WsServer) Start(natsHostPort string) error {
	ws.natsHostPort = natsHostPort

	//start listening
	if ws.isTLS() {
		if err := ws.createTlsListen(); err != nil {
			return err
		}
	} else {
		if err := ws.createHttpListen(); err != nil {
			return err
		}
	}
	ws.httpServer = &http.Server{
		ErrorLog: log.New(os.Stderr, "", log.LstdFlags),
		Handler:  http.HandlerFunc(ws.handleSession),
	}

	ws.Logger.Noticef("Listening for websocket requests on %v\n", ws.GetURL())

	go func() {
		if err := ws.httpServer.Serve(ws.listener); err != nil {
			// we orderly shutdown the server?
			if !strings.Contains(err.Error(), "http: Server closed") {
				ws.Logger.Fatalf("HTTP server error: %v\n", err)
				panic(err)
			}
		}
		ws.httpServer.Handler = nil
		ws.httpServer = nil
		ws.Logger.Noticef("HTTP server has stopped\n")
	}()

	return nil
}

func (ws *WsServer) createHttpListen() error {
	var err error
	ws.listener, err = net.Listen("tcp", ws.HostPort)
	if err != nil {
		return fmt.Errorf("cannot listen for http requests: %v", err)
	}
	// if the port was auto selected, update the config
	host, port, err := parseHostPort(ws.HostPort)
	if port < 1 {
		if err != nil {
			panic(err)
		}
		port = ws.listener.Addr().(*net.TCPAddr).Port
		ws.HostPort = fmt.Sprintf("%s:%d", host, port)
	}
	return err
}

func (ws *WsServer) makeTLSConfig() (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(ws.CertFile, ws.KeyFile)
	if err != nil {
		ws.Logger.Fatalf("error parsing key: %v", err)
	}
	cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		ws.Logger.Fatalf("error parsing cert: %v", err)
	}
	config := tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS10,
	}

	if ws.CaFile != "" {
		caCert, err := ioutil.ReadFile(ws.CaFile)
		if err != nil {
			log.Fatal(err)
		}
		caPool := x509.NewCertPool()
		if ok := caPool.AppendCertsFromPEM(caCert); !ok {
			ws.Logger.Fatalf("error parsing ca cert")
		}
		config.RootCAs = caPool
	}
	return &config, nil
}

func (ws *WsServer) createTlsListen() error {
	tlsConfig, err := ws.makeTLSConfig()
	if err != nil {
		ws.Logger.Fatalf("error generating tls config: %v", err)
	}
	config := util.CloneTLSConfig(tlsConfig)
	config.ClientAuth = tls.NoClientCert
	config.PreferServerCipherSuites = true
	config.Certificates = make([]tls.Certificate, 1)
	config.Certificates[0], err = tls.LoadX509KeyPair(ws.CertFile, ws.KeyFile)
	if err != nil {
		slog.Fatalf("error loading tls certs: %v", err)
	}
	ws.listener, err = tls.Listen("tcp", ws.HostPort, config)
	if err != nil {
		slog.Fatalf("cannot listen for http requests: %v", err)
	}

	return nil
}

var counter uint64

func (ws *WsServer) handleSession(w http.ResponseWriter, r *http.Request) {
	id := atomic.AddUint64(&counter, 1)
	upgrader := websocket.Upgrader{}
	upgrader.CheckOrigin = func(req *http.Request) bool {
		return true
	}
	c, err := upgrader.Upgrade(w, r, nil)
	defer c.Close()
	if err != nil {
		slog.Errorf("failed to upgrade ws connection: %v", err)
		return
	}

	// allow for different interfaces and random port on embedded server
	proxy, err := NewProxyWorker(id, c, ws.natsHostPort)
	if err != nil {
		slog.Errorf("failed to connect to nats: %v", err)
		return
	}
	proxy.Serve()
}

type ProxyWorker struct {
	id   uint64
	done chan string
	ws   *websocket.Conn
	tcp  net.Conn
}

func NewProxyWorker(id uint64, ws *websocket.Conn, natsHostPort string) (*ProxyWorker, error) {
	tcp, err := net.Dial("tcp", natsHostPort)
	if err != nil {
		return nil, err
	}

	proxy := ProxyWorker{}
	proxy.id = id
	proxy.done = make(chan string)
	proxy.ws = ws
	proxy.tcp = tcp

	return &proxy, nil
}

func (pw *ProxyWorker) Serve() {
	go func() {
		for {
			mt, r, err := pw.ws.NextReader()
			if err != nil {
				break
			}
			if mt == websocket.TextMessage {
				io.Copy(pw.tcp, r)
			}
			if mt == websocket.CloseMessage {
				break
			}
		}
		pw.done <- "done"
	}()

	go func() {
		buf := make([]byte, 4096)
		for {
			read, err := pw.tcp.Read(buf)
			if err != nil {
				break
			}
			if read > 0 {
				writer, err := pw.ws.NextWriter(websocket.TextMessage)
				if err != nil {
					break
				}
				writer.Write(buf[0:read])
				writer.Close()
			}
		}
		pw.done <- "done"
	}()

	<-pw.done
	pw.tcp.Close()
	pw.tcp.Close()
	<-pw.done
	slog.Noticef("ws-nats client [%d] closed", pw.id)
}

func (ws *WsServer) GetURL() string {
	protocol := "ws"
	if ws.isTLS() {
		protocol = "wss"
	}
	return fmt.Sprintf("%s://%s", protocol, ws.HostPort)
}
