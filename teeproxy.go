package main

import (
	"bytes"
	"crypto/tls"
	"flag"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"time"
)

// Console flags
var (
	listen                = flag.String("l", ":8888", "port to accept requests")
	targetProduction      = flag.String("a", "localhost:8080", "where production traffic goes. http://localhost:8080/production")
	altTarget             = flag.String("b", "localhost:8081", "where testing traffic goes. response are skipped. http://localhost:8081/test")
	debug                 = flag.Bool("debug", false, "more logging, showing ignored output")
	productionTimeout     = flag.Int("a.timeout", 2500, "timeout in milliseconds for production traffic")
	alternateTimeout      = flag.Int("b.timeout", 1000, "timeout in milliseconds for alternate site traffic")
	productionHostRewrite = flag.Bool("a.rewrite", false, "rewrite the host header when proxying production traffic")
	alternateHostRewrite  = flag.Bool("b.rewrite", false, "rewrite the host header when proxying alternate site traffic")
	percent               = flag.Float64("p", 100.0, "float64 percentage of traffic to send to testing")
	tlsPrivateKey         = flag.String("key.file", "", "path to the TLS private key file")
	tlsCertificate        = flag.String("cert.file", "", "path to the TLS certificate file")
)


var LOG_FILE = "/var/log/teeproxy/teeproxy.log"

var Logger log.Logger

func initLogging() {
	file, err := os.OpenFile(LOG_FILE, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatalln("Failed to open log file", LOG_FILE, ":", err)
	}

	Logger = *log.New(file, "", log.LstdFlags | log.Lshortfile)
}


// Sets the request URL.
//
// This turns a inbound request (a request without URL) into an outbound request.
func setRequestTarget(request *http.Request, target *string) {
	URL, err := url.Parse("http://" + *target + request.URL.String())
	if err != nil {
		Logger.Print(err)
	}
	request.URL = URL
}


// Sends a request and returns the response.
func handleRequest(request *http.Request, timeout time.Duration) (*http.Response) {
	transport := &http.Transport{
		// NOTE(girone): DialTLS is not needed here, because the teeproxy works
		// as an SSL terminator.
		Dial: (&net.Dialer{  // go1.8 deprecated: Use DialContext instead
			Timeout: timeout,
			KeepAlive: 10 * timeout,
		}).Dial,
		// Always close connections to the alternative and production servers.
		DisableKeepAlives: true,
		//IdleConnTimeout: timeout,  // go1.8
		TLSHandshakeTimeout: timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: timeout,
	}
	// Do not use http.Client here, because it's higher level and processes
	// redirects internally, which is not what we want.
	//client := &http.Client{
	//	Timeout: timeout,
	//	Transport: transport,
	//}
	//response, err := client.Do(request)
	response, err := transport.RoundTrip(request)
	if err != nil {
		Logger.Print("Request failed:", err)
	}
	return response
}

// handler contains the address of the main Target and the one for the Alternative target
type handler struct {
	Target      string
	Alternative string
	Randomizer  rand.Rand
}

// ServeHTTP duplicates the incoming request (req) and does the request to the
// Target and the Alternate target discading the Alternate response
func (h handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var productionRequest, alternativeRequest *http.Request
	if *percent == 100.0 || h.Randomizer.Float64()*100 < *percent {
		alternativeRequest, productionRequest = DuplicateRequest(req)
		go func() {
			defer func() {
				if r := recover(); r != nil && *debug {
					Logger.Print("Recovered in ServeHTTP(alternate request) from:", r)
				}
			}()

			setRequestTarget(alternativeRequest, altTarget)

			if *alternateHostRewrite {
				alternativeRequest.Host = h.Alternative
			}

			timeout := time.Duration(*alternateTimeout) * time.Millisecond
			// This keeps responses from the alternative target away from the outside world.
			_ = handleRequest(alternativeRequest, timeout)
		}()
	} else {
		productionRequest = req
	}
	defer func() {
		if r := recover(); r != nil && *debug {
			Logger.Print("Recovered in ServeHTTP(production request) from:", r)
		}
	}()

	setRequestTarget(productionRequest, targetProduction)

	if *productionHostRewrite {
		productionRequest.Host = h.Target
	}

	timeout := time.Duration(*productionTimeout) * time.Millisecond
	resp := handleRequest(productionRequest, timeout)

	if resp != nil {
		defer resp.Body.Close()

		// Forward response headers.
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)

		// Forward response body.
		body, _ := ioutil.ReadAll(resp.Body)
		w.Write(body)
	}
}


func main() {
	initLogging()

	flag.Parse()

	Logger.Printf("Starting teeproxy at %s sending to A: %s and B: %s",
	              *listen, *targetProduction, *altTarget)

	runtime.GOMAXPROCS(runtime.NumCPU())

	var err error

	var listener net.Listener

	if len(*tlsPrivateKey) > 0 {
		cer, err := tls.LoadX509KeyPair(*tlsCertificate, *tlsPrivateKey)
		if err != nil {
			Logger.Printf("Failed to load certficate: %s and private key: %s", *tlsCertificate, *tlsPrivateKey)
			return
		}

		config := &tls.Config{Certificates: []tls.Certificate{cer}}
		listener, err = tls.Listen("tcp", *listen, config)
		if err != nil {
			Logger.Printf("Failed to listen to %s: %s", *listen, err)
			return
		}
	} else {
		listener, err = net.Listen("tcp", *listen)
		if err != nil {
			Logger.Printf("Failed to listen to %s: %s", *listen, err)
			return
		}
	}

	h := handler{
		Target:      *targetProduction,
		Alternative: *altTarget,
		Randomizer:  *rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	server := &http.Server{
		Handler: h,
	}
	server.Serve(listener)
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
