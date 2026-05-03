package obs

import (
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"strings"
)

// RecoverAndExit recovers from panic, logs a single line (stack flattened), and exits 1.
// Register as the last defer in main() so it runs first on panic: defer obs.RecoverAndExit()
func RecoverAndExit() {
	r := recover()
	if r == nil {
		return
	}
	st := strings.ReplaceAll(string(debug.Stack()), "\n", " | ")
	line := fmt.Sprintf("panic: %v | %s", r, st)
	if L != nil {
		L.Info("fatal_panic", "detail", line)
	} else {
		log.Print(line)
	}
	os.Exit(1)
}
