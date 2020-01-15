# WebSocket NATS Proxy

This project is for development purposes only. NATS server is soon receiving native wss:// support.
Until that native websocket support is officially released, this allows for integration of the
browser-based nats.ws library.

`wsgnatsd` starts a websocket server and proxies to an embedded or remote NATS server.

## WebSocket Server

This server is just a proxy server to a NATS server. The NATS server can be the embedded
server or a remote server (from `-nh`). WebSocket connections are proxied over to the
embedded NATS Server or the hostport specified by `-nh`. Note that the connection between
WS and NATS server is *always* in the clear.

## Embedded NATS Server

A full NATS Server. Additional options can be passed to it by passing `--` followed by any
allowed NATS server options. By default ports for the embedded NATS server are auto selected,
if a `-c` or `-config` option is specified, the server will reject port/host port options.

## Flags

```bash
  -D debug - enable debugging output
  -V trace - enable tracing output
  -c string
     configuration file
  -h string
     ws-host - default is 127.0.0.1:4219
  -nh string
     nats-host - disables embedded NATS and proxies requests to the specified hostport
  -ca string
     cafile - ca certificate file for asset and ws servers
  -cert string
     certfile - certificate file for asset and ws servers
  -key string
     keyfile - certificate key for asset and ws servers
  -text
     textframes - use text websocket frames
```

## Examples

```bash
wsgnatsd -c /path/to/conf -- -c /path/to/natsserver.conf -DV
wsgnatsd -h 127.0.0.1:8080
wsgnatsd -nh localhost:4222
```
