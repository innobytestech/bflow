// Package release sabe cómo se publican los binarios de bflow (nombres,
// archivos, checksums, notas) y cómo se actualiza el binario instalado. Lo
// usan el script de publicación (scripts/dist.go) y bflow update, para que
// ambos coincidan siempre.
package release

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Target es una plataforma para la que se publica un binario.
type Target struct{ OS, Arch string }

// Targets son las plataformas publicadas.
var Targets = []Target{
	{"windows", "amd64"}, {"windows", "arm64"},
	{"linux", "amd64"}, {"linux", "arm64"},
	{"darwin", "amd64"}, {"darwin", "arm64"},
}

// Checksums es el archivo con el SHA-256 de cada archivo publicado.
const Checksums = "checksums.txt"

// BinaryName es el nombre del ejecutable en la plataforma.
func BinaryName(goos string) string {
	if goos == "windows" {
		return "bflow.exe"
	}
	return "bflow"
}

// AssetName es el archivo publicado para una versión y plataforma:
// bflow_0.1.0_windows_amd64.zip, bflow_0.1.0_linux_arm64.tar.gz.
func AssetName(version, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("bflow_%s_%s_%s%s", strings.TrimPrefix(version, "v"), goos, goarch, ext)
}

// Archive empaqueta el binario y los archivos extra (licencia, README) en
// dst, en zip o tar.gz según la extensión.
func Archive(dst, binary, goos string, extras ...string) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	files := append([]string{binary}, extras...)
	names := append([]string{BinaryName(goos)}, baseNames(extras)...)
	if strings.HasSuffix(dst, ".zip") {
		err = writeZip(f, files, names)
	} else {
		err = writeTarGz(f, files, names)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func baseNames(ps []string) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = filepath.Base(p)
	}
	return out
}

func writeZip(w io.Writer, files, names []string) error {
	zw := zip.NewWriter(w)
	for i, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		h := &zip.FileHeader{Name: names[i], Method: zip.Deflate, Modified: modTime(p)}
		h.SetMode(0o755)
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err := fw.Write(b); err != nil {
			return err
		}
	}
	return zw.Close()
}

func writeTarGz(w io.Writer, files, names []string) error {
	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)
	for i, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		mode := int64(0o644)
		if i == 0 {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{Name: names[i], Mode: mode, Size: int64(len(b)), ModTime: modTime(p), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		if _, err := tw.Write(b); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gw.Close()
}

// Extract saca el binario de un archivo publicado.
func Extract(archive []byte, name, goos string) ([]byte, error) {
	bin := BinaryName(goos)
	if strings.HasSuffix(name, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if f.Name == bin {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("%s no trae %s", name, bin)
	}
	gr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s no trae %s", name, bin)
		}
		if err != nil {
			return nil, err
		}
		if h.Name == bin {
			return io.ReadAll(tr)
		}
	}
}

// SHA256 es el hash en hexadecimal.
func SHA256(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// FormatChecksums arma checksums.txt: "<sha256>  <archivo>" por línea, como
// sha256sum, para verificarlo también a mano.
func FormatChecksums(sums map[string]string, order []string) string {
	var b strings.Builder
	for _, n := range order {
		fmt.Fprintf(&b, "%s  %s\n", sums[n], n)
	}
	return b.String()
}

// ParseChecksums lee checksums.txt.
func ParseChecksums(s string) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 {
			out[strings.TrimPrefix(f[1], "*")] = f[0]
		}
	}
	return out
}

// Notes es la sección de la versión en CHANGELOG.md ("## v0.1.0 …" hasta la
// siguiente "## "). Una pre-release sin sección propia (v0.1.0-rc.1) usa la
// de su versión. Sin sección no se publica: las notas se escriben antes.
func Notes(changelog, version string) (string, error) {
	if n := section(changelog, version); n != "" {
		return n, nil
	}
	if base, _, pre := strings.Cut(version, "-"); pre {
		if n := section(changelog, base); n != "" {
			return n, nil
		}
	}
	return "", fmt.Errorf("CHANGELOG.md no tiene la sección «## %s»: escríbela antes de publicar", version)
}

func section(changelog, version string) string {
	var out []string
	in := false
	for _, l := range strings.Split(strings.ReplaceAll(changelog, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(l, "## ") {
			if in {
				break
			}
			f := strings.Fields(l)
			in = len(f) > 1 && f[1] == version
			continue
		}
		if in {
			out = append(out, l)
		}
	}
	if notes := strings.TrimSpace(strings.Join(out, "\n")); notes != "" {
		return notes + "\n"
	}
	return ""
}

// ErrNotRelease es una versión que no sale de una publicación (dev o una
// pseudo-versión de go install @latest sin tag).
var ErrNotRelease = errors.New("no es una versión publicada")

// Compare compara versiones vMAJOR.MINOR.PATCH: -1, 0 o 1. Una versión con
// sufijo (-rc.1, pseudo-versión) es anterior a la misma sin sufijo.
func Compare(a, b string) (int, error) {
	pa, sa, err := parse(a)
	if err != nil {
		return 0, err
	}
	pb, sb, err := parse(b)
	if err != nil {
		return 0, err
	}
	for i := range 3 {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1, nil
			}
			return 1, nil
		}
	}
	switch {
	case sa == sb:
		return 0, nil
	case sa == "":
		return 1, nil
	case sb == "":
		return -1, nil
	case sa < sb:
		return -1, nil
	}
	return 1, nil
}

func parse(v string) ([3]int, string, error) {
	var n [3]int
	s, ok := strings.CutPrefix(v, "v")
	if !ok {
		return n, "", fmt.Errorf("%q: %w", v, ErrNotRelease)
	}
	s, _, _ = strings.Cut(s, "+") // metadatos de compilación (+dirty)
	core, suffix, _ := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return n, "", fmt.Errorf("%q: %w", v, ErrNotRelease)
	}
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil {
			return n, "", fmt.Errorf("%q: %w", v, ErrNotRelease)
		}
		n[i] = x
	}
	return n, suffix, nil
}

// pseudo es el final de una pseudo-versión de Go: fecha y commit
// (v0.0.0-20260928221113-0a0c342abcde, v0.1.1-0.20260928221113-0a0c342abcde).
var pseudo = regexp.MustCompile(`\d{14}-[0-9a-f]{12}$`)

// Published dice si v viene de un tag publicado y no de un commit suelto.
func Published(v string) bool {
	_, suffix, err := parse(v)
	return err == nil && !pseudo.MatchString(suffix)
}

func modTime(p string) time.Time {
	if st, err := os.Stat(p); err == nil {
		return st.ModTime()
	}
	return time.Now()
}
