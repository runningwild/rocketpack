package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

var (
	domain      = flag.String("domain", "", "Domain to serve containers from.")
	port        = flag.Int("port", 443, "Port to serve https traffic on.")
	dst         = flag.String("dst", "localhost:8080", "Address to forward traffic to.")
	tlsCertFile = flag.String("cert-file", "", "PEM encoded certificates.")
	tlsKeyFile  = flag.String("key-file", "", "PEM encoded key.")
)

func main() {
	flag.Parse()

	go func() {
		for {
			time.Sleep(time.Second * 5)
			resp, err := http.Get("http://www.google.com")
			if err != nil {
				log.Printf("failed to find google: %v", err)
				continue
			}
			log.Printf("found google, got response")
			data, _ := ioutil.ReadAll(resp.Body)
			log.Printf("resp: %s\n", data)
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

	r.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		data, _ := json.MarshalIndent(req, "", "  ")
		fmt.Printf("%s\n", data)
	})

	log.Fatalf("%v", http.ListenAndServeTLS(fmt.Sprintf(":%d", *port), *tlsCertFile, *tlsKeyFile, r))
}
