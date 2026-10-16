//go:build !unix

package optimize

import (
	"errors"
	"os"
)

// Processes can't be suspended here: a paused job is stopped and starts again later.
func suspend(*os.Process) error { return errors.ErrUnsupported }

func resume(*os.Process) error { return errors.ErrUnsupported }
