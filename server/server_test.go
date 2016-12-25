package server_test

import (
	"testing"

	"fmt"
	"github.com/boltdb/bolt"
	"github.com/runningwild/rocketpack/server"
	. "github.com/smartystreets/goconvey/convey"
	"os"
)

func TestServer(t *testing.T) {
	Convey("Server", t, func() {
		testFile := "test.db"
		So(os.Remove(testFile), ShouldBeNil)
		db, err := bolt.Open(testFile, 0664, nil)
		So(err, ShouldBeNil)
		So(db, ShouldNotBeNil)
		s := server.Make(db)
		ids := []server.ID{
			{Name: "a-name", Version: "1.0.1", Os: "linux", Arch: "amd64"},
			{Name: "a-name", Version: "2.0.0", Os: "linux", Arch: "amd64"},
			{Name: "a-name", Version: "1.1.0", Os: "linux", Arch: "amd64"},
			{Name: "another-name", Version: "3.4.5", Os: "linux", Arch: "amd64"},
			{Name: "yet-another-name", Version: "1.2.3", Os: "linux", Arch: "amd64"},
			{Name: "just-text", Version: "1.0.0", Os: "", Arch: ""},
		}
		Convey("can put stuff", func() {
			for i, id := range ids {
				data := []byte(fmt.Sprintf("data-%d", i))
				sig := []byte(fmt.Sprintf("sig-%d", i))
				So(s.Put(id, data, sig), ShouldBeNil)
			}
			Convey("and get that same stuff back", func() {
				for i, id := range ids {
					data := []byte(fmt.Sprintf("data-%d", i))
					sig := []byte(fmt.Sprintf("sig-%d", i))
					data, err := s.Get(id, ".aci")
					So(err, ShouldBeNil)
					So(string(data), ShouldEqual, string(data))
					data, err = s.Get(id, ".aci.asc")
					So(err, ShouldBeNil)
					So(string(data), ShouldEqual, string(sig))
				}
				latestData, err := s.Get(server.ID{Name: "a-name", Version: "latest", Os: "linux", Arch: "amd64"}, ".aci")
				So(err, ShouldBeNil)
				So(string(latestData), ShouldEqual, "data-1")
				latestSig, err := s.Get(server.ID{Name: "a-name", Version: "latest", Os: "linux", Arch: "amd64"}, ".aci.asc")
				So(err, ShouldBeNil)
				So(string(latestSig), ShouldEqual, "sig-1")
			})
			Convey("and can get back os- and arch-inspecific stuff regardless of specified os and arch", func() {
				ids := []server.ID{
					{Name: "just-text", Version: "1.0.0", Os: "", Arch: ""},
					{Name: "just-text", Version: "1.0.0", Os: "darwin", Arch: "ppc"},
					{Name: "just-text", Version: "1.0.0", Os: "", Arch: "ppc"},
					{Name: "just-text", Version: "latest", Os: "", Arch: ""},
					{Name: "just-text", Version: "latest", Os: "darwin", Arch: "ppc"},
					{Name: "just-text", Version: "latest", Os: "", Arch: "ppc"},
				}
				for _, id := range ids {
					data, err := s.Get(id, ".aci")
					So(err, ShouldBeNil)
					So(string(data), ShouldEqual, "data-5")
					sig, err := s.Get(id, ".aci.asc")
					So(err, ShouldBeNil)
					So(string(sig), ShouldEqual, "sig-5")

				}
			})
			Convey("and cannot get stuff back that doesn't exist", func() {
				_, err := s.Get(server.ID{Name: "a-name", Version: "1.4.5", Os: "linux", Arch: "amd64"}, ".aci")
				So(err, ShouldNotBeNil)
				_, err = s.Get(server.ID{Name: "a-name", Version: "1.0.1", Os: "darwin", Arch: "amd64"}, ".aci")
				So(err, ShouldNotBeNil)
			})
			Convey("and list that same stuff", func() {
				list, err := s.List("a-name")
				So(err, ShouldBeNil)
				So(len(list), ShouldEqual, 3)
				var listStr []string
				for _, id := range list {
					listStr = append(listStr, id.String())
				}
				for _, id := range ids[0:3] {
					So(listStr, ShouldContain, id.String())
				}
			})
		})
	})
}
