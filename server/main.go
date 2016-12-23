package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/appc/spec/schema/types"
	"github.com/boltdb/bolt"
	"github.com/coreos/go-semver/semver"
	"github.com/gorilla/mux"
)

var (
	domain = flag.String("domain", "", "Domain to serve containers from.")
	dbPath = flag.String("db", "db", "Database to use for persistent storage.")
	port   = flag.Int("port", 8080, "Port to serve traffic on.")
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

	db, err := bolt.Open(*dbPath, 0664, nil)
	if err != nil {
		log.Fatalf("Failed to open bolt db: %v", err)
	}
	defer db.Close()

	r := mux.NewRouter()

	sr := r.Path(fetchPattern).Subrouter()
	sr.Methods("GET").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		log.Printf("Get")
		vars, err := validateACIVars(mux.Vars(req), true)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("vars: %v\n", vars)

		// Check if it exists
		var data []byte
		bestMatch := ""
		var highestVersion *semver.Version
		if err := db.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(vars.name))
			if b == nil {
				return fmt.Errorf("bucket %q not found", vars.name)
			}
			b.ForEach(func(k, v []byte) error {
				vx, err := keyToVars(string(k))
				if err != nil {
					log.Printf("invalid key %q in %q", k, vars.name)
					return nil
				}
				if vx.name != vars.name {
					log.Printf("unexpected vars %v in %s", vx, vars.name)
					return nil
				}

				if vars.os != "" && vars.os != vx.os {
					return nil
				}
				if vars.arch != "" && vars.arch != vx.arch {
					return nil
				}
				if vars.version != "" && vars.version != vx.version {
					return nil
				}
				if vx.ext != vars.ext {
					return nil
				}

				// Now if the version was not specified,
				if vars.version == "" {
					vxSemver, err := semver.NewVersion(vx.version)
					if err != nil {
						log.Printf("container %q %q has invalid version number %q", vars.name, k, vx.version)
						return nil
					}
					if highestVersion == nil || highestVersion.LessThan(*vxSemver) {
						highestVersion = vxSemver
					} else {
						return nil
					}
				}
				bestMatch = string(k)
				return nil
			})
			if bestMatch == "" {
				return fmt.Errorf("not found")
			}
			data = b.Get([]byte(bestMatch))
			if data == nil {
				return fmt.Errorf("not found")
			}
			return nil
		}); err != nil {
			log.Printf("failed to get container %v: %v", vars, err)
			http.Error(w, "failed to get container", http.StatusInternalServerError)
			return
		}

		if _, err := w.Write(data); err != nil {
			log.Printf("failed to write data %v: %v", vars, err)
		}
	})

	sr.Methods("POST").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		log.Printf("Post")
		vars, err := validateACIVars(mux.Vars(req), false)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fmt.Printf("vars: %v\n", vars)

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

		ascFile, _, err := req.FormFile("asc")
		if err != nil {
			log.Printf("no asc file in upload: %v", err)
			http.Error(w, "no asc file found", http.StatusInternalServerError)
			return
		}

		if err := db.Batch(func(tx *bolt.Tx) error {
			bucket, err := tx.CreateBucketIfNotExists([]byte(vars.name))
			if err != nil {
				return err
			}

			vars.ext = ".aci"
			key := varsToKey(vars)
			aciData, err := ioutil.ReadAll(aciFile)
			if err != nil {
				return fmt.Errorf("failed to read aci file data: %v", err)
			}
			if err := bucket.Put(key, aciData); err != nil {
				return fmt.Errorf("failed to put aci data: %v", err)
			}

			vars.ext = ".aci.asc"
			key = varsToKey(vars)
			ascData, err := ioutil.ReadAll(ascFile)
			if err != nil {
				return fmt.Errorf("failed to read asc file data: %v", err)
			}
			if err := bucket.Put(key, ascData); err != nil {
				return fmt.Errorf("failed to put asc data: %v", err)
			}
			log.Printf("Wrote data %d %d\n", len(aciData), len(ascData))
			// TODO: verify the signature

			return nil
		}); err != nil {
			log.Printf("failed to store container: %v", err)
			http.Error(w, "failed to store container", http.StatusInternalServerError)
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

	metaReply := fmt.Sprintf(metaTemplate, *domain)
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

func validateACIVars(vars map[string]string, extension bool) (*aciVars, error) {
	if _, err := types.NewACIdentifier(vars["name"]); err != nil {
		return nil, fmt.Errorf("invalid ACIdentifier %q, must match the regexp %q", vars["name"], types.ValidACIdentifier)
	}

	if _, err := semver.NewVersion(vars["version"]); vars["version"] != "" && err != nil {
		return nil, fmt.Errorf("invalid version %q, must be a valid semver string or the empty string", vars["version"])
	}

	if vars["os"] != "" && vars["arch"] == "" {
		return nil, fmt.Errorf("os cannot be specified without specifying arch")
	}

	if extension && (vars["ext"] != ".aci" && vars["ext"] != ".aci.asc") {
		return nil, fmt.Errorf("invalid extension %q, must be '.aci' or '.aci.asc'", vars["ext"])
	}
	if v, ok := vars["ext"]; (ok && v != ".") && !extension {
		return nil, fmt.Errorf("extension was specified when not expected")
	}

	return &aciVars{name: vars["name"], version: vars["version"], os: vars["os"], arch: vars["arch"], ext: vars["ext"]}, nil
}

type aciVars struct {
	name, version, os, arch, ext string
}

const metaTemplate = `
<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="ac-discovery" content="rocketpack.io https://%s/{name}$/{version}/{os}/{arch}.{ext}" />
  </head>
</html>
`

const fetchPattern = `/{name:[^$]+}$/{version}/{os}/{arch:[^.]+}{ext:[.].*}`
