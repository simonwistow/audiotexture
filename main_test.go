package audiotexture_test

import (
	"os"
	"testing"

	"github.com/asticode/go-astiav"
)

func TestMain(m *testing.M) {
	// libx264 and the mp4 muxer are chatty on stderr, and an example's output
	// is compared on stdout, so the logs would only be noise in the report.
	astiav.SetLogLevel(astiav.LogLevelQuiet)
	os.Exit(m.Run())
}
