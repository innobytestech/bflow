package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// isTerminal indica si f es una consola interactiva.
func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// readSecret lee un secreto. En una consola no muestra lo que se escribe; si
// la entrada viene de un pipe, lee la primera línea (útil en CI).
func readSecret(in io.Reader, errw io.Writer, prompt string) (string, error) {
	f, isFile := in.(*os.File)
	if isFile && isTerminal(f) {
		fmt.Fprint(errw, prompt)
		restore := disableEcho(f)
		defer func() {
			restore()
			fmt.Fprintln(errw)
		}()
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("no se recibió el token: %w", err)
	}
	return strings.TrimSpace(line), nil
}
