package server

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
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

	"github.com/aricart/nats-server-embed/nse"
	"github.com/gorilla/websocket"
	"github.com/nats-cloud/nats-service-site/slog"
	"github.com/nats-io/gnatsd/util"
)

type Server struct {
	HostPort string
	CertFile string
	KeyFile  string
	CaFile   string

	httpServer   *http.Server
	listener     net.Listener
	EmbeddedNats *nse.NatsServer
}

func (s *Server) Start(natsArgs []string) error {
	var err error
	s.EmbeddedNats, err = nse.Start(natsArgs)
	if err != nil {
		s.Shutdown()
		return err
	}
	err = s.startHttp()
	if err != nil {
		s.Shutdown()
		return err
	}

	return nil
}

func (s *Server) Shutdown() {
	if s.EmbeddedNats.Server != nil {
		s.EmbeddedNats.Server.Shutdown()
	}
	if s.httpServer != nil {
		s.httpServer.Shutdown(nil)
	}
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

func (s *Server) createHttpListen() error {
	var err error
	s.listener, err = net.Listen("tcp", s.HostPort)
	if err != nil {
		return fmt.Errorf("[ERR] cannot listen for http requests: %v", err)
	}
	// if the port was auto selected, update the config
	host, port, err := parseHostPort(s.HostPort)
	if port < 1 {
		if err != nil {
			panic(err)
		}
		port = s.listener.Addr().(*net.TCPAddr).Port
		s.HostPort = fmt.Sprintf("%s:%d", host, port)
	}
	return err
}

func (s *Server) makeTLSConfig() (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(s.CertFile, s.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("error loading X509 certificate/key pair: %v", err)
	}
	cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("error parsing certificate: %v", err)
	}
	config := tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS10,
	}

	if s.CaFile != "" {
		caCert, err := ioutil.ReadFile(s.CaFile)
		if err != nil {
			log.Fatal(err)
		}
		caPool := x509.NewCertPool()
		if ok := caPool.AppendCertsFromPEM(caCert); !ok {
			return nil, fmt.Errorf("error parsing ca cert")
		}
		config.RootCAs = caPool
	}
	return &config, nil
}

func (s *Server) createTlsListen() error {
	tlsConfig, err := s.makeTLSConfig()
	if err != nil {
		m := slog.Errorf("Error generating tls config: %v", err)
		return errors.New(m)
	}
	config := util.CloneTLSConfig(tlsConfig)
	config.ClientAuth = tls.NoClientCert
	config.PreferServerCipherSuites = true
	config.Certificates = make([]tls.Certificate, 1)
	config.Certificates[0], err = tls.LoadX509KeyPair(s.CertFile, s.KeyFile)
	if err != nil {
		m := slog.Errorf("Error loading TLS certs: %v", err)
		return errors.New(m)
	}
	s.listener, err = tls.Listen("tcp", s.HostPort, config)
	if err != nil {
		m := slog.Errorf("Cannot listen for http requests: %v", err)
		return errors.New(m)
	}

	return nil
}

func (s *Server) isTLS() bool {
	return s.CertFile != ""
}

func (s *Server) getWsURL() string {
	protocol := "ws"
	if s.isTLS() {
		protocol = "wss"
	}
	return fmt.Sprintf("%s://%s", protocol, s.HostPort)
}

func (s *Server) startHttp() error {
	//start listening
	if s.isTLS() {
		if err := s.createTlsListen(); err != nil {
			return err
		}
	} else {
		if err := s.createHttpListen(); err != nil {
			return err
		}
	}
	s.httpServer = &http.Server{
		ErrorLog: log.New(os.Stderr, "", log.LstdFlags),
		Handler:  http.HandlerFunc(s.handleSession),
	}
	log.Printf("[INF] starting websocket proxy at: [%v]\n", s.getWsURL())

	go func() {
		if err := s.httpServer.Serve(s.listener); err != nil {
			// we orderly shutdown the server?
			if !strings.Contains(err.Error(), "http: Server closed") {
				panic(fmt.Errorf("[ERR] http server: %v\n", err))
			}
		}
		s.httpServer.Handler = nil
		s.httpServer = nil
		log.Printf("HTTP server has stopped\n")
	}()

	return nil
}

var counter uint64

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	id := atomic.AddUint64(&counter, 1)
	upgrader := websocket.Upgrader{}
	upgrader.CheckOrigin = func(req *http.Request) bool {
		return true
	}
	c, err := upgrader.Upgrade(w, r, nil)
	defer c.Close()
	if err != nil {
		log.Printf("[ERR] upgrading ws connection: %v\n", err)
		return
	}

	// allow for different interfaces and random port on embedded server
	proxy, err := NewProxyWorker(id, c, string(s.EmbeddedNats.Server.Addr().String()))
	if err != nil {
		log.Printf("[ERR] connecting to NATS: %v\n", err)
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
	log.Printf("wsnats client [%d] closed", pw.id)
}
