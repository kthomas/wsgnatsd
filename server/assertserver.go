package server

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/mitchellh/go-homedir"
)

type AssetsServer struct {
	*Opts
	httpServer *http.Server
	listener   net.Listener
}

func NewAssetsServer(conf *Opts) (*AssetsServer, error) {
	if conf.Port == 0 {
		return nil, nil
	}
	d, err := homedir.Expand(conf.Dir)
	if err != nil {
		return nil, err
	}

	d, err = filepath.Abs(d)
	if err != nil {
		return nil, err
	}

	conf.Dir = d
	fi, err := os.Stat(conf.Dir)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%q is not a directory", conf.Dir)
	}

	conf.Logger.Noticef("serving assets from %q", conf.Dir)

	var server AssetsServer
	server.Opts = conf
	return &server, nil
}

func (s *AssetsServer) filter() http.Handler {
	protocol := "ws"
	if s.Opts.CertFile != "" {
		protocol = "wss"
	}
	WSURL := fmt.Sprintf("%s://%s", protocol, s.WSHostPort)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// FIXME: this is a simple SSI include function, likely has security issues
		// FIXME: this is non portable since the file access code assumes slashes
		fp := filepath.Join(s.Opts.Dir, r.URL.Path)
		// make the request path absolute
		fp, err := filepath.Abs(fp)
		if err != nil {
			s.Logger.Errorf("failed to calculate abs for %q: %v", fp, err)
			http.Error(w, "", http.StatusBadRequest)
			return
		}

		// if the calculated path doesn't begin with the servers data dir,
		// we are outside the servers data directory
		if strings.Index(fp, s.Opts.Dir) != 0 {
			s.Logger.Errorf("request outside data dir %q", fp)
			http.Error(w, "", http.StatusBadRequest)
			return
		}

		f, err := os.Open(fp)
		if err != nil {
			if os.IsNotExist(err) {
				s.Logger.Noticef("GET %q 404", fp)
				http.Error(w, "", http.StatusNotFound)
				return
			}
			s.Logger.Errorf("%v", fp, err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}

		defer f.Close()

		d, err := ioutil.ReadAll(f)
		if err != nil {
			s.Logger.Errorf("error reading %q: %v", fp, err)
		}
		d = bytes.ReplaceAll(d, []byte("{{WSURL}}"), []byte(WSURL))

		s.Logger.Noticef("GET %q", fp)
		w.Write(d)
	})
}

func (s *AssetsServer) Start() error {
	hp := fmt.Sprintf("0.0.0.0:%d", s.Port)
	if s.Opts.CertFile != "" {
		l, err := createTlsListen(hp, s.Opts.CertFile, s.Opts.KeyFile, s.Opts.CaFile)
		if err != nil {
			return err
		}
		s.listener = l
	} else {
		l, err := createListen(hp)
		if err != nil {
			return err
		}
		s.listener = l
	}
	s.httpServer = &http.Server{
		ErrorLog: log.New(os.Stderr, "ws", log.LstdFlags),
		Handler:  s.filter(),
	}
	s.Logger.Noticef("Listening for asset requests on %v\n", hostPort(s.listener))

	go func() {
		if err := s.httpServer.Serve(s.listener); err != nil {
			// we orderly shutdown the server?
			if !strings.Contains(err.Error(), "http: server closed") {
				s.Logger.Fatalf("Asset server error: %v\n", err)
				panic(err)
			}
		}
		s.httpServer.Handler = nil
		s.httpServer = nil
		s.Logger.Noticef("Asset server has stopped\n")
	}()

	return nil
}

func (ws *AssetsServer) Shutdown() {
	if ws.httpServer != nil {
		ws.httpServer.Shutdown(nil)
	}
}
