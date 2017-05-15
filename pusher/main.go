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

const testServer = "http://localhost:8080"

var (
	protocol = flag.String("protocol", "https", "http or https")
	server   = flag.String("server", "rocketpack.io", "server to push to.")
	aciPath  = flag.String("aci", "", "aci to upload.")
	ascPath  = flag.String("asc", "", "asc to upload, defaults to aci path + '.asc'.")
)

func main() {
	flag.Parse()
	log.SetFlags(log.Lshortfile | log.Ltime)
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
	labels["sig"] = string(asc)

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
		for name, data := range map[string][]byte{"aci": aci, "asc": []byte(labels["sig"])} {
			mwriter, err := mpw.CreateFormFile(name, name)
			if err != nil {
				log.Fatalf("Unable to encode %s to file for upload: %v", name, err)
			}
			if _, err := io.Copy(mwriter, bytes.NewBuffer(data)); err != nil {
				log.Fatalf("Unable to encode %s to file for upload: %v", name, err)
			}
		}

		boundary = mpw.Boundary()
		if err := mpw.Close(); err != nil {
			log.Fatalf("Error closing multipart writer: %v", err)
		}
	}
	// s := `/{name:[^$]+}$/{version}/{os}/{arch}.{ext}`
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s://%s/%s$/%s/%s/%s.", *protocol, *server, labels["name"], labels["version"], labels["os"], labels["arch"]), body)
	log.Printf("req: %s\n", req.URL)
	req.Header.Set("Content-Type", fmt.Sprintf("multipart/form-data; boundary=%s", boundary))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("Failed to upload aci: %v", err)
	}
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}
	log.Printf("Resp:\n%s\n", data)
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
	// if !strings.HasPrefix(im.Name.String(), server+"/") && server != testServer {
	// 	return nil, nil, fmt.Errorf("Image name is %q which is not part of the server %q.", im.Name, server)
	// }
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
