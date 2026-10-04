package vars

import (
	"errors"

	"github.com/digital-dream-labs/vector-go-sdk/pkg/vector"
)

var guidLocation string = "/run/vic-cloud/perRuntimeToken"

func GetGUID() (string, error) {
	return ReadFile(guidLocation)
}

func GetVec() (*vector.Vector, error) {
	guid, err := ReadFile(guidLocation)
	if err != nil {
		return nil, errors.New("empty perruntimetoken")
	}
	return vector.New(
		vector.WithToken(guid),
		vector.WithTarget("localhost:443"),
	)
}
