package cli

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	setConsoleMode = kernel32.NewProc("SetConsoleMode")
)

const enableEchoInput = 0x0004

// disableEcho apaga el eco de la consola y devuelve cómo restaurarlo.
func disableEcho(f *os.File) func() {
	h := syscall.Handle(f.Fd())
	var mode uint32
	if err := syscall.GetConsoleMode(h, &mode); err != nil {
		return func() {}
	}
	setConsoleMode.Call(uintptr(h), uintptr(mode&^enableEchoInput))
	return func() { setConsoleMode.Call(uintptr(h), uintptr(mode)) }
}

const enableVirtualTerminalProcessing = 0x0004

// enableVT activa las secuencias de color en la consola de Windows. Devuelve
// false si no se puede (consola vieja): el banner sale sin color.
func enableVT(f *os.File) bool {
	if f == nil {
		return false
	}
	h := syscall.Handle(f.Fd())
	var mode uint32
	if err := syscall.GetConsoleMode(h, &mode); err != nil {
		return false
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return true
	}
	r, _, _ := setConsoleMode.Call(uintptr(h), uintptr(mode|enableVirtualTerminalProcessing))
	return r != 0
}

var getConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")

// termRows es el alto visible de la consola (0 si no se sabe).
func termRows(f *os.File) int {
	if f == nil {
		return 0
	}
	var info struct {
		Size, Cursor             [2]int16
		Attr                     uint16
		Left, Top, Right, Bottom int16
		MaxX, MaxY               int16
	}
	r, _, _ := getConsoleScreenBufferInfo.Call(f.Fd(), uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0
	}
	return int(info.Bottom-info.Top) + 1
}
