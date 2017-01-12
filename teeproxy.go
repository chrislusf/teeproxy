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
	listen                = flag.String("l", ":8888", "port to accept requests")
	targetProduction      = flag.String("a", "localhost:8080", "where production traffic goes. http://localhost:8080/production")
	altTarget             = flag.String("b", "localhost:8081", "where testing traffic goes. response are skipped. http://localhost:8081/test")
	debug                 = flag.Bool("debug", false, "more logging, showing ignored output")
	productionTimeout     = flag.Int("a.timeout", 3, "timeout in seconds for production traffic")
	alternateTimeout      = flag.Int("b.timeout", 1, "timeout in seconds for alternate site traffic")
	productionHostRewrite = flag.Bool("a.rewrite", false, "rewrite the host header when proxying production traffic")
	alternateHostRewrite  = flag.Bool("b.rewrite", false, "rewrite the host header when proxying alternate site traffic")
	percent               = flag.Float64("p", 100.0, "float64 percentage of traffic to send to testing")
	tlsPrivateKey         = flag.String("key.file", "", "path to the TLS private key file")
	tlsCertificate        = flag.String("cert.file", "", "path to the TLS certificate file")
)

// handler contains the address of the main Target and the one for the Alternative target
type handler struct {
	Target      string
	Alternative string
	Randomizer  rand.Rand
}

// ServeHTTP duplicates the incoming request (req) and does the request to the Target and the Alternate target discading the Alternate response
func (h handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var productionRequest, alternativeRequest *http.Request
	if *percent == 100.0 || h.Randomizer.Float64()*100 < *percent {
		alternativeRequest, productionRequest = DuplicateRequest(req)
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
			if *alternateHostRewrite {
				alternativeRequest.Host = h.Alternative
			}
			err = clientHttpConn.Write(alternativeRequest) // Pass on the request
			if err != nil {
				if *debug {
					fmt.Printf("Failed to send to %s: %v\n", h.Alternative, err)
				}
				return
			}
			_, err = clientHttpConn.Read(alternativeRequest) // Read back the reply
			if err != nil && err != httputil.ErrPersistEOF {
				if *debug {
					fmt.Printf("Failed to receive from %s: %v\n", h.Alternative, err)
				}
				return
			}
		}()
	} else {
		productionRequest = req
	}
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
	if *productionHostRewrite {
		productionRequest.Host = h.Target
	}
	err = clientHttpConn.Write(productionRequest) // Pass on the request
	if err != nil {
		fmt.Printf("Failed to send to %s: %v\n", h.Target, err)
		return
	}
	resp, err := clientHttpConn.Read(productionRequest) // Read back the reply
	if err != nil && err != httputil.ErrPersistEOF {
		fmt.Printf("Failed to receive from %s: %v\n", h.Target, err)
		return
	}
	defer resp.Body.Close()
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	body, _ := ioutil.ReadAll(resp.Body)
	w.Write(body)
}

func main() {
	flag.Parse()

	runtime.GOMAXPROCS(runtime.NumCPU())

	var err error

	var listener net.Listener

	if len(*tlsPrivateKey) > 0 {
		cer, err := tls.LoadX509KeyPair(*tlsCertificate, *tlsPrivateKey)
		if err != nil {
			fmt.Printf("Failed to load certficate: %s and private key: %s", *tlsCertificate, *tlsPrivateKey)
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
		Target:      *targetProduction,
		Alternative: *altTarget,
		Randomizer:  *rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	http.Serve(listener, h)
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
		Proto:         request.Proto,
		ProtoMajor:    request.ProtoMajor,
		ProtoMinor:    request.ProtoMinor,
		Header:        request.Header,
		Body:          nopCloser{b1},
		Host:          request.Host,
		ContentLength: request.ContentLength,
		Close:         true,
	}
	request2 = &http.Request{
		Method:        request.Method,
		URL:           request.URL,
		Proto:         request.Proto,
		ProtoMajor:    request.ProtoMajor,
		ProtoMinor:    request.ProtoMinor,
		Header:        request.Header,
		Body:          nopCloser{b2},
		Host:          request.Host,
		ContentLength: request.ContentLength,
		Close:         true,
	}
	return
}
