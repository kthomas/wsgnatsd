package server

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"github.com/gorilla/websocket"
	"github.com/nats-io/nats-server/v2/logger"
)

type WsServer struct {
	*Opts
	httpServer   *http.Server
	listener     net.Listener
	natsHostPort string
}

func NewWsServer(opts *Opts) (*WsServer, error) {
	var server WsServer
	server.Opts = opts
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

func (ws *WsServer) Start(natsHostPort string) error {
	ws.natsHostPort = natsHostPort

	//start listening
	if ws.isTLS() {
		l, err := createTlsListen(ws.WSHostPort, ws.CertFile, ws.KeyFile, ws.CaFile)
		if err != nil {
			return err
		}
		ws.listener = l
	} else {
		l, err := createListen(ws.WSHostPort)
		if err != nil {
			return err
		}
		ws.listener = l
	}
	ws.WSHostPort = hostPort(ws.listener)

	ws.httpServer = &http.Server{
		ErrorLog: log.New(os.Stderr, "ws", log.LstdFlags),
		Handler:  http.HandlerFunc(ws.handleSession),
	}

	ws.Logger.Noticef("Listening for websocket requests on %v\n", ws.GetURL())

	frames := "binary"
	if ws.TextFrames {
		frames = "text"
	}
	ws.Logger.Noticef("Proxy is using %s ws frames\n", frames)

	go func() {
		if err := ws.httpServer.Serve(ws.listener); err != nil {
			// we orderly shutdown the server?
			if !strings.Contains(err.Error(), "http: server closed") {
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

	if server.TextFrames {
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

func printProto(a []byte) {
	for i, v := range a {
		fmt.Printf("%d '%c' %d\n", i, v, v)
	}
}

func parseInfo(proto []byte) map[string]interface{} {
	count := len(proto)
	if count > 5 {
		if (proto[0] == 'I' || proto[0] == 'i') &&
			(proto[1] == 'N' || proto[1] == 'n') &&
			(proto[2] == 'F' || proto[2] == 'f') &&
			(proto[3] == 'O' || proto[3] == 'o') &&
			(proto[4] == ' ' || proto[4] == '\t') &&
			(proto[5] == '{') {

			var infoArg []byte
			for i := range proto {
				if i+1 < count && proto[i] == '\r' && proto[i+1] == '\n' {
					infoArg = proto[5:i]
					break
				}
			}
			info := make(map[string]interface{})
			if err := json.Unmarshal(infoArg, &info); err != nil {
				return nil
			}
			return info
		}
	}
	return nil
}

func (pw *ProxyWorker) tlsRequired(proto []byte) bool {
	tlsRequired := false

	info := parseInfo(proto)
	t := info["tls_required"]
	if t != nil {
		bv, ok := t.(bool)
		if ok {
			tlsRequired = bv
		}
	}
	return tlsRequired
}

func (pw *ProxyWorker) upgrade() {
	pw.tcp = tls.Client(pw.tcp, &tls.Config{InsecureSkipVerify: true})
	conn := pw.tcp.(*tls.Conn)
	conn.Handshake()
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

	first := true
	go func() {
		buf := make([]byte, 4096)
		for {
			count, err := pw.tcp.Read(buf)
			if err != nil {
				pw.logger.Errorf("tcp read: %v", err)
				break
			}
			if first {
				first = false
				if pw.tlsRequired(buf[0:count]) {
					pw.upgrade()
				}
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
	return fmt.Sprintf("%s://%s", protocol, ws.WSHostPort)
}
