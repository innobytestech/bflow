package check

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"innobytes.tech/bflow/internal/config"
)

// BaseGit es lo que la corrida sobre la base necesita de git.
type BaseGit interface {
	BranchGit
	MergeBase(ctx context.Context, base string) (string, error)
	Tracked(ctx context.Context, paths []string) ([]string, error)
	IsIgnored(ctx context.Context, path string) (bool, error)
	// WorktreeAddLocked crea un worktree desacoplado en sha, bloqueado con
	// reason (git rechaza entonces `worktree remove --force`) y sin correr los
	// ganchos del repo (core.hooksPath=hooks).
	WorktreeAddLocked(ctx context.Context, dir, sha, reason, hooks string) error
	// WorktreeForget desbloquea el worktree dir y poda los registros sin carpeta.
	WorktreeForget(ctx context.Context, dir string) error
	// Worktrees lista las carpetas de los worktrees registrados.
	Worktrees(ctx context.Context) ([]string, error)
}

// Rerun es lo que pasó al volver a correr sobre el merge-base los archivos de
// prueba que fallaron en la rama sin que ella los tocara.
type Rerun struct {
	Base    string   `json:"base,omitempty"`    // merge-base donde se corrieron
	Files   []string `json:"files,omitempty"`   // archivos que se volvieron a correr
	Ignored []string `json:"ignored,omitempty"` // «archivo > prueba» que también fallan en la base
	Why     string   `json:"why,omitempty"`     // por qué el rojo no cuenta como de la base ("" si se ignoró)
	Secs    float64  `json:"secs,omitempty"`
	Cleanup string   `json:"cleanup,omitempty"` // lo que no se pudo limpiar del worktree temporal
}

// Accepted dice si el rojo del paso se ignoró por estar ya en la base.
func (r *Rerun) Accepted() bool { return r != nil && r.Why == "" && len(r.Ignored) > 0 }

// String resume lo ignorado en una línea.
func (r *Rerun) String() string {
	names := strings.Join(firstN(r.Ignored, 5), "; ")
	if n := len(r.Ignored) - 5; n > 0 {
		names += fmt.Sprintf("; y %d más", n)
	}
	s := fmt.Sprintf("%d prueba(s) que ya fallaban en la base %s con el mismo error, ignoradas (se volvieron a correr ahí %d archivo(s) que la rama no tocó): %s",
		len(r.Ignored), short(r.Base), len(r.Files), names)
	if r.Cleanup != "" {
		s += " · ⚠ limpieza: " + r.Cleanup
	}
	return s
}

const (
	maxRerunFiles = 25
	baseDirPrefix = "bflow-base-"
	lockReason    = "bflow: lleva un enlace a node_modules; quita el enlace antes de borrar este worktree"
)

var (
	safeTestPath = regexp.MustCompile(`^[A-Za-z0-9_./@+-]+$`)
	// vitestFatal: líneas que dicen que la corrida no fue limpia aunque haya
	// pruebas en rojo (errores fuera de una prueba, umbrales, un proceso caído,
	// no compiló o se cortó antes de correr todo).
	vitestFatal = regexp.MustCompile(`(?m)^\s*Errors\s+\d+\s+errors?\b|Unhandled (Error|Rejection)|^\s*(FATAL ERROR|fatal error)\b|` +
		`out of memory|Segmentation fault|signal: killed|^\s*Killed\s*$|^\s*panic: |\[ERROR\]|\berror TS\d+|Worker exited unexpectedly|Timeout starting \w+ runner|` +
		`\bERROR: (Coverage for|\d+ imports?\b)|^\s*(Leaks|Type Errors)\s+\d+|^\s*Test run (aborted|cancell?ed)`)
	vitestFiles = regexp.MustCompile(`(?m)^\s*Test Files\s+(.*?)\s*\((\d+)\)\s*$`)
	vitestTests = regexp.MustCompile(`(?m)^\s*Tests\s+(.*?)\s*\((\d+)\)\s*$`)
	vitestFail  = regexp.MustCompile(`^\s*FAIL\s+(.*?)\s*$`)
	failedNum   = regexp.MustCompile(`(\d+) failed`)
	// afterSummary: lo único que puede venir después del resumen.
	afterSummary = regexp.MustCompile(`^\s*(Start at|Duration)\b|^\s*npm (error|ERR!|notice|warn)\b|^\s*\(node:\d+\) `)
	separator    = regexp.MustCompile(`^\s*⎯`)
	lineCol      = regexp.MustCompile(`(\.[A-Za-z]\w*):\d+(:\d+)?\b`)
	passedNum    = regexp.MustCompile(`(\d+) passed`)
	counter      = regexp.MustCompile(`^\d+/\d+$`)
)

// testRun es lo que se lee de la salida de una corrida de pruebas.
type testRun struct {
	tests   []string            // «archivo > prueba» en rojo, sin repetir y en orden
	errs    map[string][]string // por prueba, la primera línea de cada error, normalizada
	files   []string            // archivos con pruebas en rojo, sin repetir
	total   int                 // archivos de prueba que corrió
	section string              // la parte de la salida que describe los fallos
}

// parseVitest lee la salida del reporter por defecto de vitest corrida en root.
// why dice por qué no se puede dar cuenta de cada prueba en rojo ("" si se puede).
func parseVitest(out, root string) (run testRun, why string) {
	out = escRe.ReplaceAllString(strings.ReplaceAll(out, "\r\n", "\n"), "")
	if m := vitestFatal.FindString(out); m != "" {
		return run, "la salida trae un error fuera de las pruebas o la corrida no terminó limpia: " + clip(strings.TrimSpace(m), 120)
	}
	files, tests := vitestFiles.FindAllStringSubmatchIndex(out, -1), vitestTests.FindAllStringSubmatchIndex(out, -1)
	if len(files) != 1 || len(tests) != 1 || tests[0][0] < files[0][1] {
		return run, "la salida no trae un solo resumen de vitest («Test Files … (N)» y luego «Tests … (N)»)"
	}
	for _, l := range strings.Split(out[tests[0][1]:], "\n") {
		if strings.TrimSpace(l) != "" && !afterSummary.MatchString(l) {
			return run, "después del resumen de vitest hay más salida: " + clip(strings.TrimSpace(l), 160)
		}
	}
	run.total, _ = strconv.Atoi(out[files[0][4]:files[0][5]])
	filesLine := out[files[0][2]:files[0][3]]
	failedFiles, failedTests := countFailed(filesLine), countFailed(out[tests[0][2]:tests[0][3]])
	if failedFiles+countNum(passedNum, filesLine) != run.total {
		return run, "el resumen de vitest tiene archivos que no fallaron ni pasaron (saltados o cortados, p. ej. por bail): " + clip(strings.TrimSpace(filesLine), 120)
	}
	run.errs = map[string][]string{}
	var pending []string
	inGroup, first := false, -1
	pos := 0
	for _, l := range strings.Split(out[:files[0][0]], "\n") {
		start := pos
		pos += len(l) + 1
		if m := vitestFail.FindStringSubmatch(l); m != nil {
			key, bad := failKey(m[1])
			if bad != "" {
				return run, bad
			}
			if first < 0 {
				first = start
			}
			if !inGroup {
				pending = nil
			}
			pending, inGroup = append(pending, key), true
			if _, ok := run.errs[key]; !ok {
				run.tests = append(run.tests, key)
				run.errs[key] = nil
			}
			file, _, _ := strings.Cut(key, " > ")
			if !slices.Contains(run.files, file) {
				run.files = append(run.files, file)
			}
			continue
		}
		if !inGroup || strings.TrimSpace(l) == "" {
			continue
		}
		msg := ""
		if !separator.MatchString(l) {
			msg = normalizeErr(l, root)
		}
		for _, k := range pending {
			if !slices.Contains(run.errs[k], msg) {
				run.errs[k] = append(run.errs[k], msg)
			}
		}
		inGroup = false
	}
	for _, k := range pending {
		if inGroup && !slices.Contains(run.errs[k], "") {
			run.errs[k] = append(run.errs[k], "")
		}
	}
	if len(run.tests) != failedTests || len(run.files) != failedFiles {
		return run, fmt.Sprintf("la salida nombra %d prueba(s) en rojo en %d archivo(s) y su resumen dice %d en %d: hay fallas que no se pueden atribuir (o dos pruebas con el mismo nombre)",
			len(run.tests), len(run.files), failedTests, failedFiles)
	}
	if first >= 0 {
		run.section = out[first:files[0][0]]
	}
	return run, ""
}

// failKey lee «archivo > prueba» de una línea FAIL; bad dice por qué no se puede.
func failKey(rest string) (key, bad string) {
	left, name, ok := strings.Cut(rest, " > ")
	fields := strings.Fields(left)
	name = strings.TrimSpace(name)
	if !ok || len(fields) == 0 || name == "" {
		return "", "una falla no es de una prueba con nombre (falló el archivo completo o un hook): " + clip(strings.TrimSpace(rest), 160)
	}
	file := strings.TrimPrefix(strings.ReplaceAll(fields[len(fields)-1], `\`, "/"), "./")
	if filepath.Ext(file) == "" {
		return "", "no se pudo leer el archivo de la falla: " + clip(strings.TrimSpace(rest), 160)
	}
	return file + " > " + name, ""
}

// normalizeErr deja la línea de un error comparable entre la rama y la base:
// sin la carpeta donde corrió ni números de línea y columna.
func normalizeErr(l, root string) string {
	l = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(l, `\`, "/"), "%20", " "))
	if root != "" {
		l = stripPrefixFold(l, strings.ToLower(filepath.ToSlash(filepath.Clean(root)))+"/")
	}
	return clip(lineCol.ReplaceAllString(l, "$1"), 2000)
}

func countFailed(s string) int { return countNum(failedNum, s) }

func countNum(re *regexp.Regexp, s string) int {
	if m := re.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// rerunBase decide si el rojo del paso de pruebas ya estaba en la base. Solo
// cuenta si todas las pruebas en rojo están en archivos que la rama no tocó y
// también fallan, con el mismo nombre y el mismo error, al correr esos
// archivos sobre el merge-base. Ante cualquier duda, Why dice por qué no.
func (r *Runner) rerunBase(ctx context.Context, st config.Step, out string, exit int, runErr error) *Rerun {
	rr := &Rerun{}
	no := func(why string) *Rerun {
		rr.Why = why
		return rr
	}
	switch {
	case runErr != nil:
		return no("el paso no se pudo ejecutar: " + runErr.Error())
	case exit < 0 || exit >= 128:
		return no(fmt.Sprintf("el paso no terminó solo: lo cortó una señal o un fallo del proceso (código %d)", exit))
	case shellMeta.MatchString(st.Run) || shellMeta.MatchString(st.Rerun.Run):
		return no("el comando de pruebas encadena otros comandos: su código de salida no lo explican solo las pruebas")
	}
	head, why := parseVitest(out, r.Root)
	if why != "" {
		return no(why)
	}
	if len(head.tests) == 0 {
		return no("el paso falló sin ninguna prueba en rojo")
	}
	g, ok := r.Git.(BaseGit)
	if !ok {
		return no("este repo no permite correr pruebas sobre la base (git sin worktrees)")
	}
	touched, err := BranchFiles(ctx, g, r.Base)
	if err != nil {
		return no("no se pudo calcular qué archivos tocó la rama: " + err.Error())
	}
	if len(head.files) > maxRerunFiles {
		return no(fmt.Sprintf("fallan %d archivos de prueba; se vuelven a correr en la base %d como mucho", len(head.files), maxRerunFiles))
	}
	for _, f := range head.files {
		switch {
		case !safeTestPath.MatchString(f) || !InRepo(f):
			return no("la ruta " + f + " no se puede pasar tal cual al comando")
		case TouchedBy(f, touched, true) != "":
			return no("la prueba en rojo está en " + f + ", que esta rama tocó")
		case SameDir(f, touched) != "":
			return no("la rama tocó " + SameDir(f, touched) + ", junto a " + f + ": un archivo vecino puede cambiar lo que se prueba")
		case !RepoFile(r.Root, f):
			return no("la falla en " + f + " no es de un archivo del repo")
		}
	}
	mentions, more := sectionPaths(head.section, r.Root)
	if more {
		return no("la salida de las fallas nombra más rutas de las que bflow revisa")
	}
	for _, p := range mentions {
		if t := TouchedBy(p, touched, false); t != "" {
			return no("la salida de las fallas nombra " + t + ", que esta rama tocó")
		}
	}
	if t := ToolingTouched(r.Root, []string{st.Run, st.Rerun.Run}, mentions, touched); t != "" {
		return no("la rama tocó " + t + ", que puede cambiar cómo corren las pruebas")
	}
	tracked, err := g.Tracked(ctx, head.files)
	if err != nil {
		return no("no se pudo saber qué archivos rastrea git: " + err.Error())
	}
	for _, f := range head.files {
		if !slices.Contains(tracked, f) {
			return no("la prueba en rojo está en " + f + ", que git no rastrea")
		}
	}
	if ws := workspaces(r.Root); ws != "" {
		return no("el repo usa workspaces (" + ws + "): node_modules enlaza código de la rama y la base no quedaría aislada")
	}
	share, why := r.shares(ctx, g, st.Rerun, touched)
	if why != "" {
		return no(why)
	}
	sha, err := g.MergeBase(ctx, r.Base)
	if err != nil || sha == "" {
		return no(fmt.Sprintf("no se pudo calcular el merge-base con %s: %v", r.Base, err))
	}
	rr.Base, rr.Files = sha, slices.Sorted(slices.Values(head.files))
	t0 := time.Now()
	baseOut, baseExit, wt, cleanup, why := r.runAtBase(ctx, g, sha, st.Rerun, share, rr.Files)
	rr.Secs = float64(time.Since(t0).Round(100*time.Millisecond)) / float64(time.Second)
	rr.Cleanup = cleanup
	switch {
	case why != "":
		return no(why)
	case baseExit == 0:
		return no("esos archivos pasan en la base " + short(sha) + ": el rojo lo trajo la rama")
	case baseExit < 0 || baseExit >= 128:
		return no(fmt.Sprintf("la corrida en la base no terminó sola (código %d)", baseExit))
	}
	base, why := parseVitest(baseOut, wt)
	if why != "" {
		return no("en la base: " + why)
	}
	if base.total != len(rr.Files) {
		return no(fmt.Sprintf("la corrida en la base no quedó filtrada a esos archivos: corrió %d archivo(s) de prueba y se pidieron %d", base.total, len(rr.Files)))
	}
	for _, t := range head.tests {
		bErrs, ok := base.errs[t]
		if !ok {
			return no("«" + t + "» falla en la rama y no en la base " + short(sha))
		}
		for _, e := range head.errs[t] {
			if !slices.Contains(bErrs, e) {
				return no(fmt.Sprintf("«%s» falla en la rama con un error que no tiene en la base %s: %q (en la base: %q)", t, short(sha), e, strings.Join(bErrs, " | ")))
			}
		}
	}
	rr.Ignored = head.tests
	return rr
}

// workspaces dice qué archivo declara workspaces de npm, pnpm o yarn en root ("" si ninguno).
func workspaces(root string) string {
	if _, err := os.Lstat(filepath.Join(root, "pnpm-workspace.yaml")); err == nil {
		return "pnpm-workspace.yaml"
	}
	if b, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil && strings.Contains(string(b), `"workspaces"`) {
		return "package.json"
	}
	return ""
}

// shares son las rutas de rerun.share que existen en el repo. Cada una tiene
// que estar ignorada por git, fuera de lo que tocó la rama y sin contener a otra.
func (r *Runner) shares(ctx context.Context, g BaseGit, rc *config.Rerun, touched []string) (out []string, why string) {
	list := rc.Share
	if list == nil {
		list = []string{"node_modules"}
	}
	for i, s := range list {
		s = strings.Trim(filepath.ToSlash(filepath.Clean(s)), "/")
		for _, o := range list[i+1:] {
			o = strings.Trim(filepath.ToSlash(filepath.Clean(o)), "/")
			if s == o || strings.HasPrefix(o, s+"/") || strings.HasPrefix(s, o+"/") {
				return nil, "rerun.share repite o anida " + s + " y " + o
			}
		}
		if _, err := os.Lstat(filepath.Join(r.Root, filepath.FromSlash(s))); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, "no se pudo leer " + s + ": " + err.Error()
		}
		if t := TouchedBy(s, touched, true); t != "" {
			return nil, "rerun.share incluye " + s + ", que esta rama tocó (" + t + ")"
		}
		ign, err := g.IsIgnored(ctx, s)
		if err != nil {
			return nil, "no se pudo saber si git ignora " + s + ": " + err.Error()
		}
		if !ign {
			return nil, "rerun.share incluye " + s + ", que git no ignora: llevaría a la base algo de la rama"
		}
		out = append(out, s)
	}
	return out, ""
}

// sectionPaths son las rutas que nombra la descripción de las fallas; more
// dice que nombra más de las que se guardan.
func sectionPaths(section, root string) (paths []string, more bool) {
	if section == "" {
		return nil, false
	}
	b := (&Runner{Root: root}).blame(config.Step{}, section, 1, nil)
	return slices.DeleteFunc(slices.Concat(b.Findings, b.Mentions), counter.MatchString), b.More
}

// runAtBase corre los archivos files sobre sha en un worktree temporal que
// comparte con el repo las rutas share (las carpetas por enlace, los archivos
// por copia). El worktree se quita siempre; cleanup dice lo que no se pudo quitar.
func (r *Runner) runAtBase(ctx context.Context, g BaseGit, sha string, rc *config.Rerun, share, files []string) (out string, exit int, wt, cleanup, why string) {
	// Un Ctrl+C o el cierre de la consola cortan las pruebas, no a bflow, que
	// así alcanza a quitar el enlace y el worktree (este defer corre después).
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	sweepStale(ctx, g)
	tmp, err := os.MkdirTemp("", baseDirPrefix+"*")
	if err != nil {
		return "", 0, "", "", "no se pudo crear la carpeta temporal: " + err.Error()
	}
	if long, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = long // en Windows, la ruta larga y no la 8.3 (CARLOS~1)
	}
	wt = filepath.Join(tmp, "wt")
	var links []string
	var held *os.File
	defer func() {
		if held != nil {
			held.Close()
		}
		cleanup = cleanBase(context.WithoutCancel(ctx), g, tmp, wt, links)
	}()
	if inside(tmp, r.Root) {
		return "", 0, wt, "", "la carpeta temporal " + tmp + " queda dentro del repo"
	}
	hooks := filepath.Join(tmp, "hooks")
	pid, err := os.Create(filepath.Join(tmp, "pid"))
	if err != nil {
		return "", 0, wt, "", "no se pudo preparar la carpeta temporal: " + err.Error()
	}
	held = pid // abierto mientras la corrida vive: así la siguiente sabe que no es un resto
	if _, err := pid.WriteString(strconv.Itoa(os.Getpid())); err != nil {
		return "", 0, wt, "", "no se pudo preparar la carpeta temporal: " + err.Error()
	}
	if err := os.Mkdir(hooks, 0o755); err != nil {
		return "", 0, wt, "", "no se pudo preparar la carpeta temporal: " + err.Error()
	}
	if err := g.WorktreeAddLocked(ctx, wt, sha, lockReason, hooks); err != nil {
		return "", 0, wt, "", "no se pudo crear el worktree en la base: " + err.Error()
	}
	for _, f := range files {
		if st, err := os.Lstat(filepath.Join(wt, filepath.FromSlash(f))); err != nil || !st.Mode().IsRegular() {
			return "", 0, wt, "", f + " no está en la base " + short(sha)
		}
	}
	for _, s := range share {
		src, dst := filepath.Join(r.Root, filepath.FromSlash(s)), filepath.Join(wt, filepath.FromSlash(s))
		st, err := os.Stat(src)
		if err != nil {
			return "", 0, wt, "", "no se pudo leer " + s + ": " + err.Error()
		}
		if _, err := os.Lstat(dst); err == nil {
			return "", 0, wt, "", s + " existe en la base (git lo rastrea ahí): no se comparte con la corrida"
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", 0, wt, "", "no se pudo preparar " + s + " en la base: " + err.Error()
		}
		if st.IsDir() {
			links = append(links, dst) // antes de crearlo: si mklink falla a medias, igual se quita
			if err := linkDir(src, dst); err != nil {
				return "", 0, wt, "", "no se pudo enlazar " + s + " en la base: " + err.Error()
			}
		} else if err := copyFile(src, dst); err != nil {
			return "", 0, wt, "", "no se pudo copiar " + s + " a la base: " + err.Error()
		}
	}
	out, exit, err = r.Exec(ctx, wt, rerunCommand(rc, files), "")
	select {
	case <-sig:
		return out, exit, wt, "", "se interrumpió la corrida en la base"
	default:
	}
	if err != nil {
		return out, exit, wt, "", "no se pudo correr el comando en la base: " + err.Error()
	}
	return out, exit, wt, "", ""
}

// inside dice si dir queda dentro de root (o es root), sin distinguir mayúsculas.
func inside(dir, root string) bool {
	d := strings.ToLower(filepath.ToSlash(filepath.Clean(dir))) + "/"
	r := strings.ToLower(filepath.ToSlash(filepath.Clean(root))) + "/"
	if long, err := filepath.EvalSymlinks(root); err == nil {
		if l := strings.ToLower(filepath.ToSlash(filepath.Clean(long))) + "/"; strings.HasPrefix(d, l) {
			return true
		}
	}
	return strings.HasPrefix(d, r)
}

// rerunCommand arma el comando filtrado: {files} se vuelve cada archivo, con la
// forma de each, un argumento por pieza (sin shell).
func rerunCommand(rc *config.Rerun, files []string) []string {
	each := rc.Each
	if each == "" {
		each = "{file}"
	}
	var argv []string
	for _, tok := range splitArgs(rc.Run) {
		if tok != "{files}" {
			argv = append(argv, tok)
			continue
		}
		for _, f := range files {
			for _, et := range splitArgs(each) {
				argv = append(argv, strings.ReplaceAll(et, "{file}", f))
			}
		}
	}
	return argv
}

// sweepStale limpia los worktrees de corridas anteriores que quedaron porque
// mataron a bflow: los de la carpeta temporal cuyo proceso ya no existe.
func sweepStale(ctx context.Context, g BaseGit) {
	wts, err := g.Worktrees(ctx)
	if err != nil {
		return
	}
	temp := os.TempDir()
	for _, p := range wts {
		p = filepath.FromSlash(p)
		parent := filepath.Dir(p)
		if filepath.Base(p) != "wt" || !strings.HasPrefix(filepath.Base(parent), baseDirPrefix) || !inside(parent, temp) {
			continue
		}
		if runAlive(parent) {
			continue
		}
		_ = cleanBase(ctx, g, parent, p, nil)
	}
}

// cleanBase quita primero los enlaces (los que se crearon y cualquier otro que
// haya quedado adentro) y solo después borra la carpeta y suelta el worktree en
// git. No usa `git worktree remove`, que entra por un enlace y borra lo que apunta.
func cleanBase(ctx context.Context, g BaseGit, tmp, wt string, links []string) string {
	var problems []string
	for _, l := range links {
		if err := unlinkDir(l); err != nil {
			problems = append(problems, "no se pudo quitar el enlace "+l+": "+err.Error())
		}
	}
	if len(problems) == 0 {
		if err := unlinkAll(wt); err != nil {
			problems = append(problems, err.Error())
		}
	}
	admin := worktreeAdmin(wt)
	if len(problems) > 0 {
		return strings.Join(problems, "; ") + "; el worktree " + wt + " queda bloqueado en git: quita el enlace solo (`rmdir \"<enlace>\"` en cmd, sin /s), comprueba que ya no esté y después borra la carpeta y corre `git worktree unlock` y `git worktree prune`"
	}
	if err := os.RemoveAll(wt); err != nil {
		return "no se pudo borrar " + wt + ": " + err.Error()
	}
	if admin != "" {
		// Solo el registro de este worktree: un prune general poda también
		// otros worktrees del repo cuya carpeta no esté a la mano.
		if err := os.RemoveAll(admin); err != nil {
			problems = append(problems, "no se pudo soltar el worktree "+wt+" en git ("+admin+"): "+err.Error())
		}
	} else if err := g.WorktreeForget(ctx, wt); err != nil {
		problems = append(problems, "no se pudo soltar el worktree "+wt+" en git: "+err.Error())
	}
	if err := os.RemoveAll(tmp); err != nil {
		problems = append(problems, "no se pudo borrar "+tmp+": "+err.Error())
	}
	return strings.Join(problems, "; ")
}

// worktreeAdmin es la carpeta de .git/worktrees que registra el worktree wt
// ("" si no se puede leer o no apunta de vuelta a wt).
func worktreeAdmin(wt string) string {
	b, err := os.ReadFile(filepath.Join(wt, ".git"))
	if err != nil {
		return ""
	}
	d, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir: ")
	if !ok {
		return ""
	}
	d = filepath.Clean(filepath.FromSlash(d))
	if !filepath.IsAbs(d) || filepath.Base(filepath.Dir(d)) != "worktrees" {
		return ""
	}
	if gd, err := os.ReadFile(filepath.Join(d, "gitdir")); err != nil || !samePath(strings.TrimSpace(string(gd)), filepath.Join(wt, ".git")) {
		return ""
	}
	return d
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(filepath.FromSlash(a)), filepath.Clean(filepath.FromSlash(b)))
}

// unlinkAll quita todo enlace simbólico o junction bajo dir sin entrar en él.
func unlinkAll(dir string) error {
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("no se pudo revisar %s: %w", p, err)
		}
		if p == dir {
			return nil
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return fmt.Errorf("no se pudo revisar %s: %w", p, err)
		}
		if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0 {
			return nil
		}
		if err := unlinkDir(p); err != nil {
			return fmt.Errorf("no se pudo quitar el enlace %s: %w", p, err)
		}
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
}

// unlinkDir quita el enlace l sin entrar en lo que apunta y comprueba que ya no esté.
func unlinkDir(l string) error {
	if err := os.Remove(l); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Lstat(l); !errors.Is(err, os.ErrNotExist) {
		return errors.New("sigue en disco")
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	o, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(o, in); err != nil {
		o.Close()
		return err
	}
	return o.Close()
}

func short(sha string) string { return sha[:min(8, len(sha))] }

func firstN(s []string, n int) []string { return s[:min(n, len(s))] }
