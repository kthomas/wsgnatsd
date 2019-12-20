package server

import (
	"bytes"
	"context"
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
	s *http.Server
	l net.Listener
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
	l, err := CreateListen(hp, s.Opts)
	if err != nil {
		return err
	}

	s.l = l
	s.s = &http.Server{
		ErrorLog: log.New(os.Stderr, "ws", log.LstdFlags),
		Handler:  s.filter(),
	}
	s.Logger.Noticef("Listening for asset requests on %v\n", hostPort(s.l))

	go func() {
		if err := s.s.Serve(s.l); err != nil {
			// we orderly shutdown the s?
			if !strings.Contains(err.Error(), "Server closed") {
				s.Logger.Fatalf("assetserver error: %v\n", err)
				panic(err)
			}
		}
		s.s.Handler = nil
		s.s = nil
		s.Logger.Noticef("asset server has stopped\n")
	}()

	return nil
}

func (s *AssetsServer) Shutdown() {
	if s.s != nil {
		s.s.Shutdown(context.TODO())
	}
}
