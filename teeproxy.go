package main

import (
	"bytes"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"runtime"
	"time"
)

// Console flags
var (
	listen  = flag.String("l", ":8888", "port to accept requests")
	percent = flag.Float64("p", 100.0, "float64 percentage of traffic to send to testing")

	targetA  = flag.String("a", "localhost:8080", "where production traffic goes. http://localhost:8080/production")
	timeoutA = flag.Int("a.timeout", 3, "timeout in seconds for production traffic")
	rewriteA = flag.Bool("a.rewrite", false, "rewrite the host header when proxying production traffic")

	targetB  = flag.String("b", "localhost:8081", "where testing traffic goes. response are skipped. http://localhost:8081/test")
	timeoutB = flag.Int("b.timeout", 1, "timeout in seconds for alternate site traffic")
	rewriteB = flag.Bool("b.rewrite", false, "rewrite the host header when proxying alternate site traffic")

	tlsKey = flag.String("key.file", "", "path to the TLS private key file")
	tlsCer = flag.String("cert.file", "", "path to the TLS certificate file")

	debug = flag.Bool("debug", false, "more logging, showing ignored output")
)

// handler contains the address of the main Target and the one for the Alternative target
type handler struct {
	TargetA string
	TargetB string
	Factor  rand.Rand
}

// ServeHTTP duplicates the incoming request (req) and does the request to the Target and the Alternate target discading the Alternate response
func (h handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var requestA, requestB *http.Request
	if *percent == 100.0 || h.Factor.Float64()*100 < *percent {
		requestA, requestB = DuplicateRequest(req)
		go func() {
			defer func() {
				if r := recover(); r != nil && *debug {
					fmt.Println("Recovered in f", r)
				}
			}()

			// Open new TCP connection to the server
			clientTCPConn, err := net.DialTimeout("tcp", h.TargetB, time.Duration(*timeoutB)*time.Second)
			if err != nil {
				if *debug {
					fmt.Printf("Failed to connect to %s\n", h.TargetB)
				}
				return
			}

			clientHTTPConn := httputil.NewClientConn(clientTCPConn, nil) // Start a new HTTP connection on it

			defer func() { // Close the connection to the server
				if cerr := clientHTTPConn.Close(); cerr != nil {
					fmt.Print(cerr)
				}
			}()

			if *rewriteB {
				requestB.Host = h.TargetB
			}

			err = clientHTTPConn.Write(requestB) // Pass on the request
			if err != nil {
				if *debug {
					fmt.Printf("Failed to send to %s: %v\n", h.TargetB, err)
				}
				return
			}

			_, err = clientHTTPConn.Read(requestB) // Read back the reply
			if err != nil {
				if *debug {
					fmt.Printf("Failed to receive from %s: %v\n", h.TargetB, err)
				}
				return
			}
		}()
	} else {
		requestA = req
	}

	defer func() {
		if r := recover(); r != nil && *debug {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Open new TCP connection to the server
	clientTCPConn, err := net.DialTimeout("tcp", h.TargetA, time.Duration(*timeoutA)*time.Second)
	if err != nil {
		fmt.Printf("Failed to connect to %s\n", h.TargetA)
		return
	}

	clientHTTPConn := httputil.NewClientConn(clientTCPConn, nil) // Start a new HTTP connection on it

	defer func() { // Close the connection to the server
		if cerr := clientHTTPConn.Close(); cerr != nil {
			fmt.Print(cerr)
		}
	}()

	if *rewriteA {
		requestA.Host = h.TargetA
	}

	err = clientHTTPConn.Write(requestA) // Pass on the request
	if err != nil {
		fmt.Printf("Failed to send to %s: %v\n", h.TargetA, err)
		return
	}

	resp, err := clientHTTPConn.Read(requestA) // Read back the reply
	if err != nil {
		fmt.Printf("Failed to receive from %s: %v\n", h.TargetA, err)
		return
	}

	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			fmt.Print(cerr)
		}
	}()

	for k, v := range resp.Header {
		w.Header()[k] = v
	}

	w.WriteHeader(resp.StatusCode)

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Print(err)
	}

	_, werr := w.Write(body)
	if werr != nil {
		fmt.Print(werr)
	}
}

func main() {
	flag.Parse()

	runtime.GOMAXPROCS(runtime.NumCPU())

	var err error
	var cer tls.Certificate
	var listener net.Listener

	if len(*tlsKey) > 0 {
		cer, err = tls.LoadX509KeyPair(*tlsCer, *tlsKey)
		if err != nil {
			fmt.Printf("Failed to load certficate: %s and private key: %s", *tlsCer, *tlsKey)
			return
		}

		config := &tls.Config{Certificates: []tls.Certificate{cer}}
		listener, err = tls.Listen("tcp", *listen, config)
		if err != nil {
			fmt.Printf("Failed to listen to %s: %s\n", *listen, err)
			return
		}
	} else {
		listener, err = net.Listen("tcp", *listen)
		if err != nil {
			fmt.Printf("Failed to listen to %s: %s\n", *listen, err)
			return
		}
	}

	h := handler{
		TargetA: *targetA,
		TargetB: *targetB,
		Factor:  *rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	err = http.Serve(listener, h)
	if err != nil {
		fmt.Print(err)
	}
}

type nopCloser struct {
	io.Reader
}

func (nopCloser) Close() error { return nil }

// DuplicateRequest is not documented.
func DuplicateRequest(request *http.Request) (requestA *http.Request, requestB *http.Request) {
	b1 := new(bytes.Buffer)
	b2 := new(bytes.Buffer)
	w := io.MultiWriter(b1, b2)
	_, err := io.Copy(w, request.Body)
	if err != nil {
		fmt.Print(err)
	}

	defer func() {
		if err := request.Body.Close(); err != nil {
			fmt.Print(err)
		}
	}()

	requestA = &http.Request{
		Method:        request.Method,
		URL:           request.URL,
		Proto:         request.Proto,
		ProtoMajor:    request.ProtoMajor,
		ProtoMinor:    request.ProtoMinor,
		Header:        request.Header,
		Body:          nopCloser{b1},
		Host:          request.Host,
		ContentLength: request.ContentLength,
	}

	requestB = &http.Request{
		Method:        request.Method,
		URL:           request.URL,
		Proto:         request.Proto,
		ProtoMajor:    request.ProtoMajor,
		ProtoMinor:    request.ProtoMinor,
		Header:        request.Header,
		Body:          nopCloser{b2},
		Host:          request.Host,
		ContentLength: request.ContentLength,
	}

	return
}
