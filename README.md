# WebSocket NATS Server

A WebSocket Server embedding a [NATS](https://nats.io/) server used for development
while NATS server gets own native WS/S support.

This server has 3 different servers:

- Asset Server
- WebSocket Server
- NATS Server


## Asset Server

Is an HTTP server that can serve content from a document root. The server processes all files 
and replaces `{{WSURL}}` with the WebSocket URL for the WS server.

To enable asset serving specify the `-p` with a port and the `-d` with the document root.


## WebSocket Server

This server is just a proxy server to a NATS server. The NATS server can be the embedded
server or a remote server (from `-rhp`). WebSocket connections are proxied over to the
embedded NATS Server or the hostport specified by `-rhp`. Note that the connection between
WS and NATS server is *always* in the clear.


## NATS Server

A full NATS Server. Additional options can be passed to it by passing `--` followed by any
allowed NATS server options. By default ports for the embedded NATS server are auto selected,
if a `-c` or `-config` option is specified, the server will reject port/host port options.


## Flags
```
  -D	debug - enable debugging output
  -V	trace - enable tracing output
  -c string
    	configuration file
  -ca string
    	cafile - ca certificate file for asset and ws servers
  -cert string
    	certfile - certificate file for asset and ws servers
  -d string
    	dir - asset directory, requires depends port
  -key string
    	keyfile - certificate key for asset and ws servers
  -p int
    	port - http port for asset server default is no asset server (default -1)
  -pid string
    	piddir - piddir path for auto assigned port information (default "/var/folders/tk/05x7yv5n1z1c5s6r89y6gd0w0000gn/T/")
  -rhp string
    	remotenatshostport - disables embedded NATS and proxies requests to the specified hostport
  -text
    	textframes - use text websocket frames
  -whp string
    	wshostport - (default is auto port) (default "127.0.0.1:0")
```

## Examples

```bash
wsgnatsd -p 80 -d /tmp/docroot
wsgnatsd -c /path/to/conf -- -c /path/to/natsserver.conf -DV
wsgnatsd -p 80 -whp 127.0.0.1:8080
wsgnatsd -rhp localhost:4222
```



