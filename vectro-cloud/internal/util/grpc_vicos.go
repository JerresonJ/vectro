// +build vicos

package util

import (
	"vectro/cloud/internal/robot"
)

func init() {
	if opt := robot.OSUserAgent(); opt != nil {
		platformOpts = append(platformOpts, opt)
	}
}
