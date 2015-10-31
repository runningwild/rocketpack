package rocketpack

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"

	"appengine"
	"appengine/blobstore"
	"appengine/datastore"
)

func serveError(c appengine.Context, w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	w.Header().Set("Content-Type", "text/plain")
	io.WriteString(w, "Internal Server Error")
	c.Errorf("%v", err)
}

func handlePrepareUpload(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
	uploadURL, err := blobstore.UploadURL(c, "/upload", nil)
	if err != nil {
		serveError(c, w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, "%s", uploadURL)
}

func handleServe(w http.ResponseWriter, r *http.Request) {
	blobstore.Send(w, appengine.BlobKey(r.FormValue("blobKey")))
}

type Container struct {
	Version   string
	OS        string
	Arch      string
	Signature string
	BlobKey   appengine.BlobKey
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)

	// Sanity check all of the inputs.
	blobs, other, err := blobstore.ParseUpload(r)
	if err != nil {
		serveError(c, w, err)
		return
	}
	if len(other["name"]) != 1 || len(other["version"]) != 1 || len(other["os"]) != 1 || len(other["arch"]) != 1 || len(other["signature"]) != 1 {
		serveError(c, w, fmt.Errorf("must specify all of name, version, os, arch and signature"))
		return
	}
	if len(blobs) != 1 || len(blobs["file"]) != 1 {
		log.Printf("Got blobs: %v", blobs)
		// log.Printf("Got others: %v", other)
		serveError(c, w, fmt.Errorf("must specify exactly one upload file named 'file' as the container to upload"))
		return
	}
	signature, err := ioutil.ReadAll(base64.NewDecoder(base64.URLEncoding, bytes.NewBuffer([]byte(other["signature"][0]))))
	if err != nil {
		serveError(c, w, fmt.Errorf("got an error while trying to read signature: %v", err))
		return
	}

	// Check for an existing registry, create it if necessary.
	rkey := registryKey(c, other["name"][0])
	var registry ContainerRegistry
	err = datastore.Get(c, rkey, &registry)
	if err != nil && err != datastore.ErrNoSuchEntity {
		serveError(c, w, fmt.Errorf("unable to find registry: %v", err))
		return
	}
	if err == datastore.ErrNoSuchEntity {
		if _, err := datastore.Put(c, rkey, &ContainerRegistry{Name: other["name"][0]}); err != nil {
			serveError(c, w, fmt.Errorf("failed to create container registry: %v", err))
		}
	}

	// Check for existing containers so we can delete them before adding this new one.
	// TODO: It would be better to have this new one overwrite the existing one and clean the old
	// ones up in a background process, this would avoid the annoying possibility of attempting to
	// add a container and destroying the old one without actually adding the new one.
	var existing []Container
	keys, err := datastore.NewQuery("Container").
		Ancestor(rkey).
		Filter("Version =", other["version"][0]).
		Filter("OS =", other["os"][0]).
		Filter("Arch =", other["arch"][0]).
		GetAll(c, &existing)
	if err != nil {
		serveError(c, w, fmt.Errorf("unable to query registry: %v", err))
	}
	for i := range existing {
		if err := blobstore.Delete(c, existing[i].BlobKey); err != nil {
			serveError(c, w, fmt.Errorf("unable to delete existing container: %v", err))
		}
		if err := datastore.Delete(c, keys[i]); err != nil {
			serveError(c, w, fmt.Errorf("unable to remove existing container: %v", err))
		}
	}

	// Put the container into the datastore
	key := datastore.NewIncompleteKey(c, "Container", rkey)
	entry := &Container{
		Version:   other["version"][0],
		Arch:      other["arch"][0],
		OS:        other["os"][0],
		Signature: string(signature),
		BlobKey:   blobs["file"][0].BlobKey,
	}
	if _, err := datastore.Put(c, key, entry); err != nil {
		serveError(c, w, err)
		return
	}
}

type ContainerRegistry struct {
	Name string
}

func registryKey(c appengine.Context, name string) *datastore.Key {
	key := datastore.NewKey(c, "ContainerRegistry", name, 0, nil)
	if key == nil {
		log.Printf("Didn't get a key from datastore.NewKey()")
	}
	return key
}

var matchAci = regexp.MustCompile(`^(https?://)?[^/]*/(.+)___(.+)___(.+)___(.+)\.aci(\.asc)?`)

func handleRoot(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
	if discover := r.FormValue("ac-discovery"); discover == "1" {
		http.Redirect(w, r, "/meta/meta.html", http.StatusMovedPermanently)
		return
	}

	parts := matchAci.FindStringSubmatch(r.URL.String())
	if len(parts) < 7 {
		serveError(c, w, fmt.Errorf("invalid request"))
		return
	}
	name := parts[2]
	version := parts[3]
	os := parts[4]
	arch := parts[5]
	asc := parts[6] == ".asc"

	var registry ContainerRegistry
	rkey := registryKey(c, name)
	if err := datastore.Get(c, rkey, &registry); err != nil {
		serveError(c, w, fmt.Errorf("unable to get reigstry %q: %v", name, err))
		return
	}
	it := datastore.NewQuery("Container").Ancestor(rkey).Run(c)
	var newest string
	var signature string
	var bkey appengine.BlobKey
	found := false
	for {
		var c Container
		_, err := it.Next(&c)
		if err == datastore.Done {
			break
		}
		if err != nil {
			serveError(c, w, err)
			return
		}
		if c.OS != "" && c.OS != os {
			continue
		}
		if c.Arch != "" && c.Arch != arch {
			continue
		}
		if version == c.Version {
			bkey = c.BlobKey
			signature = c.Signature
			found = true
			break
		}
		if version == "latest" {
			if version > newest {
				found = true
				newest = version
				bkey = c.BlobKey
				signature = c.Signature
			}
		}
	}
	if !found {
		serveError(c, w, fmt.Errorf("failed to find container"))
		return
	}
	if asc {
		fmt.Fprintf(w, signature)
		return
	}
	blobstore.Send(w, bkey)
}

func init() {
	http.HandleFunc("/prepareupload", handlePrepareUpload)
	http.HandleFunc("/upload", handleUpload)
	http.HandleFunc("/serve/", handleServe)
	http.HandleFunc("/", handleRoot)
}
