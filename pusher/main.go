package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"strings"

	"github.com/appc/spec/schema"
)

const testServer = "localhost:8080"

var (
	server  = flag.String("server", "rocketpack.io", "server to push to.")
	aciPath = flag.String("aci", "", "aci to upload.")
	ascPath = flag.String("asc", "", "asc to upload, defaults to aci path + '.asc'.")
)

func main() {
	flag.Parse()
	if *aciPath == "" {
		log.Fatalf("Must specify an aci with --aci.")
	}
	if *ascPath == "" {
		*ascPath = *aciPath + ".asc"
	}
	aci, labels, err := loadAndValidateACI(*aciPath, *server)
	if err != nil {
		log.Fatalf("Unable to load aci %q: %v", *aciPath, err)
	}

	asc, err := ioutil.ReadFile(*ascPath)
	if err != nil {
		log.Fatalf("Unable to load asc %q: %v", *ascPath, err)
	}

	resp, err := http.Get(fmt.Sprintf("http://%s/prepareupload", *server))
	if err != nil {
		log.Fatalf("Failed to upload to server: %v", err)
	}
	target, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Got invalid response from server: %v", err)
	}
	signatureBuf := bytes.NewBuffer(nil)
	{
		enc := base64.NewEncoder(base64.URLEncoding, signatureBuf)
		if _, err := io.Copy(enc, bytes.NewBuffer(asc)); err != nil {
			log.Fatalf("Failed to encode signature: %v", err)
		}
		enc.Close()
	}

	body := bytes.NewBuffer(nil)
	var boundary string
	{
		mpw := multipart.NewWriter(body)
		mwriter, err := mpw.CreateFormFile("file", "file.aci")
		if err != nil {
			log.Fatalf("Unable to encode aci to file for upload: %v", err)
		}
		if _, err := io.Copy(mwriter, bytes.NewBuffer(aci)); err != nil {
			log.Fatalf("Unable to encode aci to file for upload: %v", err)
		}
		labels["signature"] = string(signatureBuf.Bytes())
		for _, key := range []string{"name", "version", "os", "arch", "signature"} {
			w, err := mpw.CreateFormField(key)
			if err != nil {
				log.Fatalf("Failed to write form field %q: %v", key, err)
			}
			if _, err := io.Copy(w, bytes.NewBuffer([]byte(labels[key]))); err != nil {
				log.Fatalf("Failed to write form field %q: %v", key, err)
			}
		}
		boundary = mpw.Boundary()
		if err := mpw.Close(); err != nil {
			log.Fatalf("Error closing multipart writer: %v", err)
		}
	}

	req, _ := http.NewRequest("POST", fmt.Sprintf("%s", target), body)
	req.Header.Set("Content-Type", fmt.Sprintf("multipart/form-data; boundary=%s", boundary))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("Failed to upload aci: %v", err)
	}
}

func loadAndValidateACI(path, server string) (data []byte, labels map[string]string, err error) {
	rc, err := openFileMaybeGzipped(path)
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to open %q: %v", path, err)
	}
	defer rc.Close()
	tr := tar.NewReader(rc)
	var manifest []byte
	var foundRootfs bool
	for {
		header, err := tr.Next()
		if err != nil {
			break
		}
		if header.Name == "manifest" {
			buf := bytes.NewBuffer(nil)
			if _, err := io.Copy(buf, tr); err != nil {
				return nil, nil, fmt.Errorf("Failed reading archive: %v", err)
			}
			manifest = buf.Bytes()
		} else if header.Name == "rootfs" {
			foundRootfs = true
		} else if !strings.HasPrefix(header.Name, "rootfs/") {
			return nil, nil, fmt.Errorf("Invalid aci, contains unexpected filename: %q.", header.Name)
		}
	}
	if !foundRootfs {
		return nil, nil, fmt.Errorf("Didn't find rootfs.")
	}
	var im schema.ImageManifest
	if err := im.UnmarshalJSON(manifest); err != nil {
		return nil, nil, fmt.Errorf("Failed to parse manifest: %v", err)
	}
	labels = make(map[string]string)
	for _, label := range im.Labels {
		switch label.Name.String() {
		case "version":
			labels["version"] = label.Value
		case "os":
			labels["os"] = label.Value
		case "arch":
			labels["arch"] = label.Value
		}
	}
	if labels["version"] == "" {
		return nil, nil, fmt.Errorf("Unspecified version is not supported.")
	}
	if !strings.HasPrefix(im.Name.String(), server+"/") && server != testServer {
		return nil, nil, fmt.Errorf("Image name is %q which is not part of the server %q.", im.Name, server)
	}
	labels["name"] = im.Name.String()
	data, err = ioutil.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("Unable to read file %q: %v", path, err)
	}
	return data, labels, nil
}

func openFileMaybeGzipped(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	g, err := gzip.NewReader(f)
	if err != nil {
		f.Seek(0, 0)
		return f, nil
	}
	return g, nil
}
