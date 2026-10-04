//go:build !cgo

package loguploader

import "errors"

// UploadDebugLogs sent logs to DDL's cloud; Vectro doesn't upload logs.
func UploadDebugLogs() (string, error) {
	return "", errors.New("vectro: log upload disabled")
}
