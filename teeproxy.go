package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"runtime"
	"time"
)

// Console flags
var (
	listen           = flag.String("l", ":8888", "port to accept requests")
	targetProduction = flag.String("a", "localhost:8080", "where production traffic goes. http://localhost:8080/production")
	altTarget        = flag.String("b", "localhost:8081", "where testing traffic goes. response are skipped. http://localhost:8081/test")
	debug            = flag.Bool("debug", false, "more logging, showing ignored output")
)

// handler contais the address of the main Target and the one for the Alternative target
type handler struct {
	Target      string
	Alternative string
}

// ServeHTTP duplicates the incoming request (req) and does the request to the Target and the Alternate target discading the Alternate response
func (h handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	req1, req2 := DuplicateRequest(req)
	go func() {
		defer func() {
			if r := recover(); r != nil && *debug {
				fmt.Println("Recovered in f", r)
			}
		}()
		client_tcp_conn, _ := net.DialTimeout("tcp", h.Alternative, time.Duration(1*time.Second)) // Open new TCP connection to the server
		client_http_conn := httputil.NewClientConn(client_tcp_conn, nil)                          // Start a new HTTP connection on it
		client_http_conn.Write(req1)                                                              // Pass on the request
		client_http_conn.Read(req1)                                                               // Read back the reply
		client_http_conn.Close()                                                                  // Close the connection to the server
	}()
	defer func() {
		if r := recover(); r != nil && *debug {
			fmt.Println("Recovered in f", r)
		}
	}()

	client_tcp_conn, _ := net.DialTimeout("tcp", h.Target, time.Duration(3*time.Second)) // Open new TCP connection to the server
	client_http_conn := httputil.NewClientConn(client_tcp_conn, nil)                     // Start a new HTTP connection on it
	client_http_conn.Write(req2)                                                         // Pass on the request
	resp, _ := client_http_conn.Read(req2)                                               // Read back the reply
	resp.Write(w)                                                                        // Write the reply to the original connection
	client_http_conn.Close()                                                             // Close the connection to the server
}

func main() {
	flag.Parse()

	runtime.GOMAXPROCS(runtime.NumCPU())

	local, _ := net.Listen("tcp", *listen)
	h := handler{
		Target:      *targetProduction,
		Alternative: *altTarget,
	}
	http.Serve(local, h)
}

type nopCloser struct {
	io.Reader
}

func (nopCloser) Close() error { return nil }

func DuplicateRequest(request *http.Request) (request1 *http.Request, request2 *http.Request) {
	b1 := new(bytes.Buffer)
	b2 := new(bytes.Buffer)
	w := io.MultiWriter(b1, b2)
	io.Copy(w, request.Body)
	defer request.Body.Close()
	request1 = &http.Request{
		Method:        request.Method,
		URL:           request.URL,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        request.Header,
		Body:          nopCloser{b1},
		Host:          request.Host,
		ContentLength: request.ContentLength,
	}
	request2 = &http.Request{
		Method:        request.Method,
		URL:           request.URL,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        request.Header,
		Body:          nopCloser{b2},
		Host:          request.Host,
		ContentLength: request.ContentLength,
	}
	return
}
