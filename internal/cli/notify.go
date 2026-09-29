package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"innobytes.tech/bflow/internal/engine"
	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
)

// Aviso al humano cuando una tarea espera su decisión. Corre al terminar el
// turno de la sesión principal (hook Stop): si Claude pregunta algo y la
// persona está en otra ventana, la notificación del sistema la trae de vuelta.
// Un aviso por espera; nunca en CI ni sin escritorio.

// gateAction es lo que la persona tiene que hacer en cada gate, en palabras
// llanas (el panel y las notificaciones).
var gateAction = map[string]string{
	string(flow.GateLane):        "elegir el carril",
	string(flow.GateDiscovery):   "cerrar el discovery",
	string(flow.GateSpec):        "aprobar la spec",
	string(flow.GateSplit):       "decidir si se divide la feature",
	string(flow.GateContract):    "aprobar el contrato (pruebas y firmas)",
	string(flow.GateDecision):    "tomar una decisión que pidió el agente",
	string(flow.GatePause):       "dar permiso para la revisión",
	string(flow.GateRounds):      "decidir tras varias rondas de calidad rechazadas",
	string(flow.GateQuestions):   "contestar las preguntas de producto",
	string(flow.GateWalkthrough): "recorrer el cambio y aprobar el PR",
}

// humanAction dice qué espera la tarea de la persona ("" si nada).
func humanAction(v engine.View) string {
	switch {
	case v.Blocked != "":
		return "desbloquear la tarea: " + truncate(v.Blocked, 80)
	case v.Gate != "":
		if a, ok := gateAction[v.Gate]; ok {
			return a
		}
		return "decidir la gate " + v.Gate
	}
	return ""
}

// notifyCmd arma el comando que muestra la notificación. El texto va en
// variables de entorno, nunca dentro del script: así no hay que escaparlo.
func notifyCmd(goos string, getenv func(string) string, lookPath func(string) (string, error)) ([]string, error) {
	if getenv("CI") != "" {
		return nil, fmt.Errorf("%w: CI", errNoDesktop)
	}
	switch goos {
	case "windows":
		// Toast de Windows con el AppID de Windows PowerShell (5.1 trae WinRT).
		const ps = `[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] > $null
$x = New-Object Windows.Data.Xml.Dom.XmlDocument
$t = [Security.SecurityElement]::Escape($env:BFLOW_TITLE); $b = [Security.SecurityElement]::Escape($env:BFLOW_BODY)
$x.LoadXml("<toast><visual><binding template='ToastGeneric'><text>$t</text><text>$b</text></binding></visual></toast>")
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe').Show([Windows.UI.Notifications.ToastNotification]::new($x))`
		return []string{"powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps}, nil
	case "darwin":
		if getenv("SSH_CONNECTION") != "" {
			return nil, fmt.Errorf("%w: sesión SSH", errNoDesktop)
		}
		return []string{"osascript", "-e", `display notification (system attribute "BFLOW_BODY") with title (system attribute "BFLOW_TITLE")`}, nil
	}
	if getenv("DISPLAY") == "" && getenv("WAYLAND_DISPLAY") == "" {
		return nil, fmt.Errorf("%w: no hay DISPLAY ni WAYLAND_DISPLAY", errNoDesktop)
	}
	if _, err := lookPath("notify-send"); err != nil {
		return nil, fmt.Errorf("%w: falta notify-send", errNoDesktop)
	}
	return []string{"sh", "-c", `exec notify-send -a bflow "$BFLOW_TITLE" "$BFLOW_BODY"`}, nil
}

// notified recuerda la última espera avisada, para no repetir el aviso en
// cada turno mientras la persona no decide.
type notified struct {
	ID    string    `json:"id"`
	What  string    `json:"what"`
	Since time.Time `json:"since"`
}

// notifyWaiting avisa si la tarea v espera a la persona y no se avisó ya de
// esa espera. Devuelve lo que avisó ("" si nada).
func notifyWaiting(storeDir string, v engine.View, send func(title, body string) error) string {
	what := humanAction(v)
	if what == "" {
		return ""
	}
	cur := notified{ID: v.ID, What: v.Gate + "|" + v.Blocked, Since: v.GateSince}
	if cur.Since.IsZero() && v.Since != nil {
		cur.Since = *v.Since
	}
	p := filepath.Join(storeDir, "cache", "notified.json")
	var last notified
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &last)
	}
	if last.ID == cur.ID && last.What == cur.What && last.Since.Equal(cur.Since) {
		return ""
	}
	body := "Espera tu decisión: " + what
	if send("bflow · "+v.ID, body) != nil {
		return ""
	}
	if b, err := json.Marshal(cur); err == nil {
		_ = store.EnsureDirFor(p)
		_ = store.WriteAtomic(p, b)
	}
	return body
}

// sendNotification muestra la notificación. Espera a que termine (con tope):
// si el hook corre en un grupo de procesos que se cierra al acabar, un
// proceso suelto moriría antes de mostrarla.
func sendNotification(title, body string) error {
	args, err := notifyCmd(goos, os.Getenv, exec.LookPath)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = append(os.Environ(), "BFLOW_TITLE="+title, "BFLOW_BODY="+body)
	return cmd.Run()
}

// notifyActive avisa de la tarea activa si espera a la persona y ui.notify
// no está apagado.
func notifyActive(e *engine.Engine) {
	if n := e.Cfg.UI.Notify; n != nil && !*n {
		return
	}
	ctx := context.Background()
	id, err := e.Active(ctx)
	if err != nil {
		return
	}
	if v, err := e.Status(ctx, id); err == nil {
		notifyWaiting(e.Store.Dir(), v, sendNotification)
	}
}
