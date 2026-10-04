//go:build !shipping && vicos
// +build !shipping,vicos

package main

import (
	"vectro/cloud/internal/log"
	"vectro/cloud/internal/robot"
)

func init() {
	certErrorFunc = onCertError
}

func onCertError() bool {
	if err := robot.WriteFaceErrorCode(850); err != nil {
		log.Println("Error writing error code (isn't it ironic?):", err)
	}
	return true
}
