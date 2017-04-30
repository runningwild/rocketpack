package server

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"

	// Imports the Google Cloud Storage client package.
	"cloud.google.com/go/storage"
	"golang.org/x/net/context"
)

type Server interface {
	Get(ctx context.Context, id ID, ext string) ([]byte, error)
	Put(ctx context.Context, id ID, data, sig []byte) error
	List(ctx context.Context, name string) ([]ID, error)
	Delete(ctx context.Context, id ID) error
}

func MakeCloudStorageServer(ctx context.Context, projectID, bucketName string) (Server, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to make client: %v", err)
	}

	if attrs, err := client.Bucket(bucketName).Attrs(ctx); err == storage.ErrBucketNotExist {
		fmt.Printf("%v %v\n", attrs, err)
		fmt.Printf("creating bucket %s\n", bucketName)
		if err := client.Bucket(bucketName).Create(ctx, projectID, nil); err != nil {
			return nil, fmt.Errorf("failed to create bucket: %v", err)
		}
	} else {
		fmt.Printf("%v %v\n", attrs, err)
		fmt.Printf("bucket attrs: %v\n", attrs)
	}
	return &cloudServer{
		client: client,
		bucket: client.Bucket(bucketName),
	}, nil
}

type cloudServer struct {
	client *storage.Client
	bucket *storage.BucketHandle
}

func (s *cloudServer) Get(ctx context.Context, id ID, ext string) ([]byte, error) {
	if err := id.Validate(); err != nil {
		return nil, fmt.Errorf("invalid id: %v", err)
	}

	obj, err := s.bucket.Object(id.String() + ext).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get object reader: %v", err)
	}
	defer obj.Close()
	data, err := ioutil.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to read object: %v", err)
	}
	return data, nil
}
func (s *cloudServer) Put(ctx context.Context, id ID, data, sig []byte) error {
	if err := id.Validate(); err != nil {
		return fmt.Errorf("invalid id: %v", err)
	}

	for key, val := range map[string][]byte{id.String() + ".aci": data, id.String() + ".aci.asc": sig} {
		obj := s.bucket.Object(key).NewWriter(ctx)
		defer obj.Close()
		if _, err := io.Copy(obj, bytes.NewBuffer(val)); err != nil {
			return fmt.Errorf("failed to write object: %v", err)
		}
	}
	return nil
}
func (s *cloudServer) List(ctx context.Context, name string) ([]ID, error) {
	it := s.bucket.Objects(ctx, &storage.Query{})
	idsmaps := make(map[ID]bool)
	for obj, err := it.Next(); err == nil; obj, err = it.Next() {
		id, _, err := stringToID(obj.Name)
		if err != nil {
			continue
		}
		idsmaps[id] = true
	}
	var ids []ID
	for id := range idsmaps {
		ids = append(ids, id)
	}
	return ids, nil
}
func (s *cloudServer) Delete(ctx context.Context, id ID) error {
	if err := id.Validate(); err != nil {
		return fmt.Errorf("invalid id: %v", err)
	}

	if err := s.bucket.Object(id.Name).Delete(ctx); err != nil {
		return fmt.Errorf("failed to delete object: %v", err)
	}
	return nil
}
