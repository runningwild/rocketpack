package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	"bytes"

	"github.com/gorilla/mux"
	"github.com/runningwild/rocketpack/server"
	"golang.org/x/net/context"
)

var (
	domain = flag.String("domain", "rocketpack.io", "Domain to serve containers from.")
	port   = flag.Int("port", 8080, "Port to serve traffic on.")
)

func main() {
	log.SetFlags(log.Lshortfile | log.Ltime | log.Ldate)
	flag.Parse()

	s, err := server.MakeCloudStorageServer(context.Background(), "montage-generator", "rocketpack-images")
	if err != nil {
		log.Fatalf("failed to create storage: %v", err)
	}

	ids, err := s.List(context.Background(), "")
	if err != nil {
		log.Fatalf("unable to list: %v", err)
	}
	log.Printf("Listing...\n")
	for _, id := range ids {
		log.Printf("%v\n", id)
	}
	log.Printf("Done listing...\n")

	r := mux.NewRouter()

	sr := r.Path(fetchPattern).Subrouter()
	sr.Methods("GET").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		log.Printf("Get")
		vars := mux.Vars(req)
		id, err := parseVars(vars)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		ext := vars["ext"]
		if ext != ".aci" && ext != ".aci.asc" {
			http.Error(w, "invalid extension", http.StatusBadRequest)
			return
		}

		log.Printf("ID: %v\n", id)
		data, err := s.Get(req.Context(), id, ext)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		io.Copy(w, bytes.NewBuffer(data))
	})

	sr.Methods("POST").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		log.Printf("Post")
		vars := mux.Vars(req)
		id, err := parseVars(vars)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if err := req.ParseMultipartForm(1 << 30); err != nil {
			log.Printf("unable to parse multipart form: %v", err)
			http.Error(w, "uploads too large", http.StatusInternalServerError)
			return
		}
		aciFile, _, err := req.FormFile("aci")
		if err != nil {
			log.Printf("no aci file in upload: %v", err)
			http.Error(w, "no aci file found", http.StatusInternalServerError)
			return
		}
		aciData, err := ioutil.ReadAll(aciFile)
		if err != nil {
			log.Printf("failed to read aci file data: %v", err)
			http.Error(w, "failed to read aci file", http.StatusInternalServerError)
			return
		}

		ascFile, _, err := req.FormFile("asc")
		if err != nil {
			log.Printf("no asc file in upload: %v", err)
			http.Error(w, "no asc file found", http.StatusInternalServerError)
			return
		}
		ascData, err := ioutil.ReadAll(ascFile)
		if err != nil {
			log.Printf("failed to read asc file data: %v", err)
			http.Error(w, "failed to read asc file", http.StatusInternalServerError)
			return
		}

		if err := s.Put(req.Context(), id, aciData, ascData); err != nil {
			log.Printf("failed to put: %v", err)
			http.Error(w, "failed put", http.StatusInternalServerError)
			return
		}
	})

	// TODO: Obviously delete requires some kind of authentication
	sr.Methods("DELETE").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// vars, err := validateACIVars(mux.Vars(req), false)
		// if err != nil {
		// 	http.Error(w, err.Error(), http.StatusBadRequest)
		// }
	})

	sr.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, "monkey\n")
	})

	metaReply := fmt.Sprintf(metaTemplate, *domain, *domain)
	r.PathPrefix("/").Queries("ac-discovery", "1").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, metaReply)
	})
	r.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, "balls")
	})

	log.Fatalf("%v", http.ListenAndServe(fmt.Sprintf(":%d", *port), r))
}

func varsToKey(vars *aciVars) []byte {
	return []byte(strings.Join([]string{vars.version, vars.os, vars.arch, vars.ext}, "$"))
}

func keyToVars(key string) (*aciVars, error) {
	parts := strings.Split(key, "$")
	if len(parts) != 4 {
		return nil, fmt.Errorf("invalid key format")
	}
	// TODO: do format checking on individual parts
	return &aciVars{version: parts[0], os: parts[1], arch: parts[2], ext: parts[3]}, nil
}

func parseVars(vars map[string]string) (server.ID, error) {
	id := server.ID{
		Name:    vars["name"],
		Version: vars["version"],
		Os:      vars["os"],
		Arch:    vars["arch"],
	}
	if err := id.Validate(); err != nil {
		return server.ID{}, err
	}
	return id, nil
}

type aciVars struct {
	name, version, os, arch, ext string
}

const metaTemplate = `
<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="ac-discovery" content="%s https://%s/{name}$/{version}/{os}/{arch}.{ext}" />
  </head>
</html>
`

const fetchPattern = `/{name:[^$]+}$/{version}/{os}/{arch:[^.]+}{ext:[.].*}`
