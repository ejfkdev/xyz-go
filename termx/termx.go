// Package termx probes the terminal/environment context shared by the CLI
// format axis and the (reserved) style axis: whether an output writer is an
// interactive terminal, and whether colour is suppressed by the environment.
// It is a leaf package (zero third-party dependencies) so both the cli
// frontend and the root dispatcher can use it without an import cycle.
//
// xyz-spec §10.7a pins format and style as two orthogonal axes that share
// this one TTY probe; termx is that shared seam.
package termx

import (
	"io"
	"os"
)

// Interactive reports whether w is an interactive terminal (TTY). A nil w
// falls back to os.Stdout. Only an *os.File that is a character device counts
// as interactive; buffers, pipes and redirected files are not. Embedding code
// that cannot stat a real device should treat the result as non-interactive.
func Interactive(w io.Writer) bool {
	if w == nil {
		w = os.Stdout
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// StdoutInteractive reports whether the process's os.Stdout is a TTY.
func StdoutInteractive() bool { return Interactive(os.Stdout) }

// NoColor reports whether colour output is suppressed by the environment:
// the NO_COLOR convention (any non-empty value) or TERM=dumb. This is the
// style axis' input (xyz-spec §10.7a); it is independent of Interactive — a
// real terminal with NO_COLOR set is interactive but colourless. Reserved for
// the colour renderer; exposed now so hosts can query it.
func NoColor() bool {
	if v := os.Getenv("NO_COLOR"); v != "" {
		return true
	}
	return os.Getenv("TERM") == "dumb"
}
