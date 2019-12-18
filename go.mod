module github.com/aricart/wsgnatsd

require (
	github.com/gorilla/websocket v1.4.1
	github.com/nats-io/nats-server/v2 v2.0.1-0.20190625001713-2db76bde3329
)

replace github.com/nats-io/nats-server/v2 v2.0.1-0.20190625001713-2db76bde3329 => /Users/aricart/Dropbox/code/go-mod-projects/nats-server

go 1.13
