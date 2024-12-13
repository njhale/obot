package version

import (
	"runtime/debug"

	"github.com/otto8-ai/nah/pkg/version"
)

var (
	Tag = "v0.0.0-dev"
)

func Get() version.Version {
	// return version.NewVersion(Tag)
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return version.NewVersion(Tag)
	}
	return version.NewVersion(bi.Main.Version)
}
