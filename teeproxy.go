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
	listen            = flag.String("l", ":8888", "port to accept requests")
	targetProduction  = flag.String("a", "localhost:8080", "where production traffic goes. http://localhost:8080/production")
	altTarget         = flag.String("b", "localhost:8081", "where testing traffic goes. response are skipped. http://localhost:8081/test")
	debug             = flag.Bool("debug", false, "more logging, showing ignored output")
	productionTimeout = flag.Int("a.timeout", 3, "timeout in seconds for production traffic")
	alternateTimeout  = flag.Int("b.timeout", 1, "timeout in seconds for alternate site traffic")
)

// handler contains the address of the main Target and the one for the Alternative target
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
		// Open new TCP connection to the server
		clientTcpConn, err := net.DialTimeout("tcp", h.Alternative, time.Duration(time.Duration(*alternateTimeout)*time.Second))
		if err != nil {
			if *debug {
				fmt.Printf("Failed to connect to %s\n", h.Alternative)
			}
			return
		}
		clientHttpConn := httputil.NewClientConn(clientTcpConn, nil) // Start a new HTTP connection on it
		defer clientHttpConn.Close()                                 // Close the connection to the server
		err = clientHttpConn.Write(req1)                             // Pass on the request
		if err != nil {
			if *debug {
				fmt.Printf("Failed to send to %s: %v\n", h.Alternative, err)
			}
			return
		}
		_, err = clientHttpConn.Read(req1) // Read back the reply
		if err != nil {
			if *debug {
				fmt.Printf("Failed to receive from %s: %v\n", h.Alternative, err)
			}
			return
		}
	}()
	defer func() {
		if r := recover(); r != nil && *debug {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Open new TCP connection to the server
	clientTcpConn, err := net.DialTimeout("tcp", h.Target, time.Duration(time.Duration(*productionTimeout)*time.Second))
	if err != nil {
		fmt.Printf("Failed to connect to %s\n", h.Target)
		return
	}
	clientHttpConn := httputil.NewClientConn(clientTcpConn, nil) // Start a new HTTP connection on it
	defer clientHttpConn.Close()                                 // Close the connection to the server
	err = clientHttpConn.Write(req2)                             // Pass on the request
	if err != nil {
		fmt.Printf("Failed to send to %s: %v\n", h.Target, err)
		return
	}
	resp, err := clientHttpConn.Read(req2) // Read back the reply
	if err != nil {
		fmt.Printf("Failed to receive from %s: %v\n", h.Target, err)
		return
	}
	resp.Write(w) // Write the reply to the original connection
}

func main() {
	flag.Parse()

	runtime.GOMAXPROCS(runtime.NumCPU())

	local, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Printf("Failed to listen to %s\n", *listen)
		return
	}
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
