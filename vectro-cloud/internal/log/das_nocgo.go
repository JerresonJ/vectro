//go:build !cgo

package log

import (
	"strconv"
	"strings"
	"time"
)

// Pure-Go build of the DAS event logger (Vectro builds vic-cloud with CGO
// disabled). Same API and output format as das.go; the markers mirror das.h.

const (
	dasEventMarker = "@"
	dasFieldMarker = "\x1F"
)

var dasStart = time.Now()

// DasFields contains the data that can be associated with an event.
type DasFields struct {
	Strings [4]string
	Ints    [4]string
}

// SetStrings fills the Strings member; only up to 4 strings are copied.
func (f *DasFields) SetStrings(strs ...string) *DasFields {
	for i := range strs {
		if i >= len(f.Strings) {
			break
		}
		f.Strings[i] = strs[i]
	}
	return f
}

// SetInts fills the Ints member; only up to 4 ints are copied.
func (f *DasFields) SetInts(ints ...int) *DasFields {
	for i := range ints {
		if i >= len(f.Ints) {
			break
		}
		f.Ints[i] = strconv.FormatInt(int64(ints[i]), 10)
	}
	return f
}

// DasEmpty logs a DAS event with only the given event name.
func DasEmpty(event string) {
	Das(event, &DasFields{})
}

// Das logs a DAS event with the given event name and extra data fields.
func Das(event string, fields *DasFields) {
	strs := strings.Join(fields.Strings[:], dasFieldMarker)
	ints := strings.Join(fields.Ints[:], dasFieldMarker)
	uptimeMS := strconv.FormatInt(time.Since(dasStart).Milliseconds(), 10)
	allFields := strings.Join([]string{event, strs, ints, uptimeMS}, dasFieldMarker)
	Printf("%s%s", dasEventMarker, allFields)
}
