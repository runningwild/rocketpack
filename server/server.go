package server

import (
	"fmt"
	"log"
	"sort"

	"github.com/boltdb/bolt"
)

type Server interface {
	Get(id ID, ext string) ([]byte, error)
	Put(id ID, data, sig []byte) error
	List(name string) ([]ID, error)
	Delete(id ID) error
}

type impl struct {
	db *bolt.DB
}

func Make(db *bolt.DB) Server {
	return &impl{db: db}
}

func (s *impl) Get(id ID, ext string) ([]byte, error) {
	ids, err := s.List(id.Name)
	if err != nil {
		return nil, err
	}
	n := 0
	for i := range ids {
		if ids[i].Version != id.Version && id.Version != "latest" {
			continue
		}
		if ids[i].Os != "" && ids[i].Os != id.Os {
			continue
		}
		if ids[i].Arch != "" && ids[i].Arch != id.Arch {
			continue
		}
		ids[n] = ids[i]
		n++
	}
	ids = ids[0:n]
	if len(ids) == 0 {
		return nil, fmt.Errorf("no matching containers found")
	}
	var data []byte
	if err := s.db.Batch(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(ids[0].Name))
		if bucket == nil {
			log.Printf("Expected bucket for name %q", ids[0].Name)
			return fmt.Errorf("no matching containers found")
		}
		data = bucket.Get([]byte(idToString(ids[0], ext)))
		if data == nil {
			return fmt.Errorf("no matching containers found")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return data, nil
}

func (s *impl) Put(id ID, data, sig []byte) error {
	if err := id.Validate(); err != nil {
		return fmt.Errorf("invalid id: %v", err)
	}
	if err := s.db.Batch(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(id.Name))
		if err != nil {
			return err
		}
		log.Printf("Putting into bucket %s", id.Name)
		aciKey := idToString(id, ".aci")
		ascKey := idToString(id, ".aci.asc")
		if err := bucket.Put([]byte(aciKey), data); err != nil {
			return fmt.Errorf("failed to put data: %v", err)
		}
		if err := bucket.Put([]byte(ascKey), sig); err != nil {
			return fmt.Errorf("failed to put signature: %v", err)
		}

		// TODO: verify the signature

		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (s *impl) List(name string) ([]ID, error) {
	if len(name) == 0 {
		return nil, fmt.Errorf("invalid name")
	}
	var ids []ID
	if err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(name))
		if bucket == nil {
			return fmt.Errorf("no containers found under the name %q", name)
		}
		log.Printf("Looking into bucket %s", name)
		if err := bucket.ForEach(func(k, _ []byte) error {
			id, ext, err := stringToID(string(k))
			if ext == ".aci.asc" {
				return nil
			}
			log.Printf("Id is %v", id)
			if err != nil {
				log.Printf("error on container id %s: %v", k, err)
				return nil
			}
			ids = append(ids, id)
			return nil
		}); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Sort(idSlice(ids))
	return ids, nil
}

func (s *impl) Delete(id ID) error {
	return nil
}
