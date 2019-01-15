teeproxy
=========

A reverse HTTP proxy that duplicates requests.

Why you may need this?
----------------------

You may have production servers running, but you need to upgrade to a new system. You want to run A/B test on both old and new systems to confirm the new system can handle the production load, and want to see whether the new system can run in shadow mode continuously without any issue.

How it works?
-------------

teeproxy is a reverse HTTP proxy. For each incoming request, it clones the request into 2 requests, forwards them to 2 servers. The results from server A are returned as usual, but the results from server B are ignored.

teeproxy handles GET, POST, and all other http methods.

Build
-------------

```
go build
```

Usage
-------------

```
 ./teeproxy -l :8888 -a [http(s)://]localhost:9000 -b [http(s)://]localhost:9001 [-b [http(s)://]localhost:9002]
```

`-l` specifies the listening port. `-a` and `-b` are meant for system A and systems B. The B systems can be taken down or started up without causing any issue to the teeproxy.

#### Configuring timeouts ####
 
It's also possible to configure the timeout to both systems

*  `-a.timeout int`: timeout in milliseconds for production traffic (default `2500`)
*  `-b.timeout int`: timeout in milliseconds for alternate site traffic (default `1000`)

#### Configuring host header rewrite ####

Optionally rewrite host value in the http request header.

*  `-a.rewrite bool`: rewrite for production traffic (default `false`)
*  `-b.rewrite bool`: rewrite for alternate site traffic (default `false`)
 
#### Configuring a percentage of requests to alternate site ####

*  `-p float64`: only send a percentage of requests. The value is float64 for more precise control. (default `100.0`)

#### Configuring HTTPS ####

*  `-key.file string`: a TLS private key file. (default `""`)
*  `-cert.file string`: a TLS certificate file. (default `""`)

#### Configuring client IP forwarding ####

It's possible to write `X-Forwarded-For` and `Forwarded` header (RFC 7239) so
that the production and alternate backends know about the clients:

*  `-forward-client-ip` (default is false)

#### Configuring connection handling ####

By default, teeproxy tries to reuse connections. This can be turned off, if the
endpoints do not support this.

*  `-close-connections` (default is false)

