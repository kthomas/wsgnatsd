# WebSocket NATS Server

A WebSocket Server embedding a [NATS](https://nats.io/) server.


```bash
wsgnatsd [-hp localhost:8080] [-cert <certfile>] [-key <keyfile>] [-ca <cacert>] [-- <gnatsd opts>]
```

The suggested use of this server is to run the `wsgnatsd` to serve as a gateway enabling the use of NATS on browsers. Websocket NATS clients such as [ws-nats](https://github.com/nats-io/ws-nats). 
 If the certificate options (`-cert`, `-key`) are provided, the server will serve securely.
 
Typically the wsgnatsd won't serve other NATS clients outside of the localhost. Other NATS servers should instead cluster to the wsgnatsd. Such topology will save one layer of encryption as the embedded WS server can exchange messages with the embedded NATS server without encrypting.

wsgnatsd doesn't limit configuration options. After passing a `--` flag, all arguments that follow are forwarded directly to the embedded NATS server, providing full access to all configuration
options available to the NATS server.