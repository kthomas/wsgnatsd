package server

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/gorilla/websocket"
	"github.com/nats-io/gnatsd/logger"
	"github.com/nats-io/gnatsd/util"
)

type Conf struct {
	HostPort string
	CertFile string
	KeyFile  string
	CaFile   string
	Text     bool
	Debug    bool
	Trace    bool
	Logger   *logger.Logger
}

type WsServer struct {
	httpServer   *http.Server
	listener     net.Listener
	natsHostPort string
	Logger       *logger.Logger
	Conf
}

func NewWsServer(conf Conf, Logger *logger.Logger) (*WsServer, error) {
	server := WsServer{}
	server.Conf = conf
	server.Logger = Logger
	return &server, nil
}

func (ws *WsServer) Shutdown() {
	if ws.httpServer != nil {
		ws.httpServer.Shutdown(nil)
	}
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

	frames := "binary"
	if ws.Text {
		frames = "text"
	}
	ws.Logger.Noticef("Proxy is handling %s ws frames\n", frames)

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
		ws.Logger.Fatalf("error loading tls certs: %v", err)
	}
	ws.listener, err = tls.Listen("tcp", ws.HostPort, config)
	if err != nil {
		ws.Logger.Fatalf("cannot listen for http requests: %v", err)
	}

	host, port, err := parseHostPort(ws.HostPort)
	if port < 1 {
		if err != nil {
			panic(err)
		}
		port = ws.listener.Addr().(*net.TCPAddr).Port
		ws.HostPort = fmt.Sprintf("%s:%d", host, port)
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
		ws.Logger.Errorf("failed to upgrade ws connection: %v", err)
		return
	}

	// allow for different interfaces and random port on embedded server
	proxy, err := NewProxyWorker(id, c, ws.natsHostPort, ws)
	if err != nil {
		ws.Logger.Errorf("failed to connect to nats: %v", err)
		return
	}
	proxy.Serve()
}

type ProxyWorker struct {
	id        uint64
	done      chan string
	ws        *websocket.Conn
	tcp       net.Conn
	frameType int
	logger    *logger.Logger
}

func NewProxyWorker(id uint64, ws *websocket.Conn, natsHostPort string, server *WsServer) (*ProxyWorker, error) {
	tcp, err := net.Dial("tcp", natsHostPort)
	if err != nil {
		return nil, err
	}

	proxy := ProxyWorker{}
	proxy.id = id
	proxy.done = make(chan string)
	proxy.ws = ws
	proxy.tcp = tcp
	proxy.logger = server.Logger

	if server.Text {
		proxy.frameType = websocket.TextMessage
	} else {
		proxy.frameType = websocket.BinaryMessage
	}

	return &proxy, nil
}

func debugFrameType(ft int) string {
	switch ft {
	case websocket.TextMessage:
		return "TEXT"
	case websocket.BinaryMessage:
		return "BIN"
	case websocket.CloseMessage:
		return "CLOSE"
	case websocket.PingMessage:
		return "PING"
	case websocket.PongMessage:
		return "PONG"
	}
	return "?"
}

func (pw *ProxyWorker) Serve() {
	go func() {
		for {
			mt, data, err := pw.ws.ReadMessage()
			if err != nil {
				pw.logger.Errorf("ws read: %v", err)
				break
			}
			pw.logger.Tracef("ws [%v] >: %v\n", debugFrameType(mt), string(data))

			if mt == websocket.TextMessage || mt == websocket.BinaryMessage {
				count, err := pw.tcp.Write(data)
				if err != nil {
					pw.logger.Errorf("tcp write: %v", err)
				}
				if count != len(data) {
					pw.logger.Errorf("tcp wrote %d instead of %d bytes", count, len(data))
				}
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
			count, err := pw.tcp.Read(buf)
			if err != nil {
				pw.logger.Errorf("tcp read: %v", err)
				break
			}
			pw.logger.Tracef("tcp >: %v\n", string(buf[0:count]))
			err = pw.ws.WriteMessage(pw.frameType, buf[0:count])
			if err != nil {
				pw.logger.Errorf("ws write: %v", err)
				break
			}
		}
		pw.done <- "done"
	}()

	<-pw.done
	pw.tcp.Close()
	pw.ws.Close()
	<-pw.done
	pw.logger.Noticef("ws-nats client [%d] closed", pw.id)
}

func (ws *WsServer) GetURL() string {
	protocol := "ws"
	if ws.isTLS() {
		protocol = "wss"
	}
	return fmt.Sprintf("%s://%s", protocol, ws.HostPort)
}
