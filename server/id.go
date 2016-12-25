package server

import (
	"fmt"
	"strings"

	"github.com/appc/spec/schema/types"
	"github.com/coreos/go-semver/semver"
)

type ID struct {
	Name    string
	Version string
	Os      string
	Arch    string
}

func (id ID) String() string {
	return idToString(id, "")
}

func idToString(id ID, ext string) string {
	str := strings.Join([]string{id.Name, id.Version, id.Os, id.Arch}, "$")
	if ext != "" {
		str += "$" + ext
	}
	return str
}

func stringToID(s string) (id ID, ext string, err error) {
	parts := strings.Split(s, "$")
	if len(parts) < 4 || len(parts) > 5 {
		err = fmt.Errorf("invalid id string")
		return
	}
	if len(parts) == 5 {
		ext = parts[4]
	}
	_id := ID{
		Name:    parts[0],
		Version: parts[1],
		Os:      parts[2],
		Arch:    parts[3],
	}
	if idErr := _id.Validate(); idErr != nil {
		err = fmt.Errorf("invalid id string: %v", err)
		return
	}
	id = _id
	return
}

func (id ID) Validate() error {
	if _, err := types.NewACIdentifier(id.Name); err != nil {
		return fmt.Errorf("invalid ACIdentifier %q, must match the regexp %q", id.Name, types.ValidACIdentifier)
	}
	if _, err := semver.NewVersion(id.Version); id.Version != "" && err != nil {
		return fmt.Errorf("invalid version %q, must be a valid semver string or the empty string", id.Version)
	}
	if id.Os != "" && id.Arch == "" {
		return fmt.Errorf("os cannot be specified without specifying arch")
	}
	return nil
}

type idSlice []ID

func (s idSlice) Len() int { return len(s) }
func (s idSlice) Less(i, j int) bool {
	vi, err := semver.NewVersion(s[i].Version)
	if err != nil {
		return false
	}
	vj, err := semver.NewVersion(s[j].Version)
	if err != nil {
		return false
	}
	return vj.LessThan(*vi)
}
func (s idSlice) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
