package server

import (
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/nats-io/nats-server/v2/conf"
	"github.com/nats-io/nats-server/v2/logger"
)

type Opts struct {
	CaFile             string
	CertFile           string
	Debug              bool
	Dir                string
	KeyFile            string
	Logger             *logger.Logger
	PidDir             string
	Port               int
	RemoteNatsHostPort string
	NatsHostPort       string
	TextFrames         bool
	Trace              bool
	WSHostPort         string
}

func DefaultOpts() Opts {
	var c Opts
	c.Port = -1
	c.WSHostPort = "127.0.0.1:0"
	c.PidDir = os.Getenv("TMPDIR")

	return c
}

func LoadOpts(fp string) (*Opts, error) {
	b, err := ioutil.ReadFile(fp)
	if err != nil {
		return nil, fmt.Errorf("error reading configuration file: %v", err)
	}
	return ParseOpts(string(b))
}

func ParseOpts(c string) (*Opts, error) {
	m, err := conf.Parse(c)
	if err != nil {
		return nil, err
	}

	var o Opts
	for k, v := range m {
		k = strings.ToLower(k)
		switch k {
		case "cafile":
			v, err := parseStringValue(k, v)
			if err != nil {
				panic(err)
			}
			o.CaFile = v
		case "certfile":
			v, err := parseStringValue(k, v)
			if err != nil {
				panic(err)
			}
			o.CertFile = v
		case "debug":
			v, err := parseBooleanValue(k, v)
			if err != nil {
				panic(err)
			}
			o.Debug = v
		case "dir":
			v, err := parseStringValue(k, v)
			if err != nil {
				panic(err)
			}
			o.Dir = v
		case "keyfile":
			v, err := parseStringValue(k, v)
			if err != nil {
				panic(err)
			}
			o.KeyFile = v
		case "piddir":
			v, err := parseStringValue(k, v)
			if err != nil {
				panic(err)
			}
			o.PidDir = v
		case "port":
			v, err := parseIntValue(k, v)
			if err != nil {
				panic(err)
			}
			o.Port = v
		case "remotenatshostport":
			v, err := parseStringValue(k, v)
			if err != nil {
				panic(err)
			}
			o.RemoteNatsHostPort = v
		case "textframes":
			v, err := parseBooleanValue(k, v)
			if err != nil {
				panic(err)
			}
			o.TextFrames = v
		case "trace":
			v, err := parseBooleanValue(k, v)
			if err != nil {
				panic(err)
			}
			o.Trace = v
		case "wshostport":
			v, err := parseStringValue(k, v)
			if err != nil {
				panic(err)
			}
			o.Dir = v
		default:
			return nil, fmt.Errorf("error parsing store configuration. Unknown field %q", k)
		}
	}
	return &o, nil
}

func parseIntValue(keyName string, v interface{}) (int, error) {
	i := 0
	var err error
	switch t := v.(type) {
	case int, int8, int16, int32, int64:
		i = t.(int)
	case string:
		i, err = strconv.Atoi(v.(string))
		if err != nil {
			err = fmt.Errorf("error parsing %s option %v as a int", keyName, v)
		}
	default:
		err = fmt.Errorf("unable to parse integer: %v", v)
	}
	return i, err
}

func parseStringValue(keyName string, v interface{}) (string, error) {
	sv, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("error parsing %s option %q", keyName, v)
	}
	return sv, nil
}

func parseBooleanValue(keyname string, v interface{}) (bool, error) {
	switch t := v.(type) {
	case bool:
		return bool(t), nil
	case string:
		sv := v.(string)
		return strings.ToLower(sv) == "true", nil
	default:
		return false, fmt.Errorf("error parsing %s option %q as a boolean", keyname, v)
	}
}
