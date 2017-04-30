package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

var (
	port        = flag.Int("port", 443, "Port to serve https traffic on.")
	dst         = flag.String("dst", "localhost:8080", "Address to forward traffic to.")
	tlsCertFile = flag.String("cert-file", "", "PEM encoded certificates.")
	tlsKeyFile  = flag.String("key-file", "", "PEM encoded key.")
)

func main() {
	flag.Parse()

	go func() {
		for {
			time.Sleep(time.Second * 25)
			_, err := http.Get("http://www.google.com")
			if err != nil {
				log.Printf("failed to find google: %v", err)
				continue
			}
			log.Printf("found google, got response")
		}
	}()

	r := mux.NewRouter()

	// Health-Check code so GCE will route requests to us.
	r.MatcherFunc(func(r *http.Request, rm *mux.RouteMatch) bool {
		agent := r.Header["User-Agent"]
		return len(agent) == 1 && strings.HasPrefix(agent[0], "GoogleHC/")
	}).HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("OK"))
	})

	r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if *dst == "" {
			return
		}
		req.Host = ""
		req.RequestURI = ""
		req.URL.Scheme = "http"
		req.URL.Host = *dst
		fmt.Printf("Req: %s\n", reqToStr(req))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Printf("Failed proxy request: %v\n", err)
			return
		}
		io.Copy(w, resp.Body)
	})

	//log.Fatalf("%v", http.ListenAndServe(fmt.Sprintf(":%d", *port), r))
	log.Fatalf("%v", http.ListenAndServeTLS(fmt.Sprintf(":%d", *port), *tlsCertFile, *tlsKeyFile, r))
}

func reqToStr(req *http.Request) string {
	data, _ := json.MarshalIndent(&Request{
		Method:           req.Method,
		URL:              req.URL,
		Proto:            req.Proto,
		ProtoMajor:       req.ProtoMajor,
		ProtoMinor:       req.ProtoMinor,
		Header:           req.Header,
		Body:             req.Body,
		ContentLength:    req.ContentLength,
		TransferEncoding: req.TransferEncoding,
		Close:            req.Close,
		Host:             req.Host,
		Form:             req.Form,
		PostForm:         req.PostForm,
		MultipartForm:    req.MultipartForm,
		Trailer:          req.Trailer,
		RemoteAddr:       req.RemoteAddr,
		RequestURI:       req.RequestURI,
		TLS:              req.TLS,
	}, "", "  ")
	return string(data)
}

type Request struct {
	Method           string
	URL              *url.URL
	Proto            string // "HTTP/1.0"
	ProtoMajor       int    // 1
	ProtoMinor       int    // 0
	Header           http.Header
	Body             io.ReadCloser
	ContentLength    int64
	TransferEncoding []string
	Close            bool
	Host             string
	Form             url.Values
	PostForm         url.Values
	MultipartForm    *multipart.Form
	Trailer          http.Header
	RemoteAddr       string
	RequestURI       string
	TLS              *tls.ConnectionState
}
