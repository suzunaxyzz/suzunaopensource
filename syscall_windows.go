//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

var (
	modkernel32 = syscall.NewLazyDLL("kernel32.dll")
	moduser32   = syscall.NewLazyDLL("user32.dll")
	modshell32  = syscall.NewLazyDLL("shell32.dll")

	procAttachConsole = modkernel32.NewProc("AttachConsole")
	procAllocConsole  = modkernel32.NewProc("AllocConsole")
	procMessageBoxW   = moduser32.NewProc("MessageBoxW")
	procShellExecuteW = modshell32.NewProc("ShellExecuteW")
)

const attachParentProcess = ^uintptr(0)

func attachConsole() bool {
	r, _, _ := procAttachConsole.Call(attachParentProcess)
	if r == 0 {
		return false
	}
	reopenStdHandles()
	return true
}

func allocConsole() bool {
	r, _, _ := procAllocConsole.Call()
	if r == 0 {
		return false
	}
	reopenStdHandles()
	return true
}

func reopenStdHandles() {
	if f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		os.Stdout = f
		os.Stderr = f
	}
	if f, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = f
	}
}

func showErrorMessageBox(title, message string) {
	const (
		mbOk        = 0x00000000
		mbIconError = 0x00000010
	)
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	msgPtr, _ := syscall.UTF16PtrFromString(message)
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(msgPtr)), uintptr(unsafe.Pointer(titlePtr)), uintptr(mbOk|mbIconError))
}

func showInfoMessageBox(title, message string) {
	const (
		mbOk       = 0x00000000
		mbIconInfo = 0x00000040
	)
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	msgPtr, _ := syscall.UTF16PtrFromString(message)
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(msgPtr)), uintptr(unsafe.Pointer(titlePtr)), uintptr(mbOk|mbIconInfo))
}

func openURL(targetUrl string) {
	verbPtr, _ := syscall.UTF16PtrFromString("open")
	urlPtr, _ := syscall.UTF16PtrFromString(targetUrl)
	procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verbPtr)), uintptr(unsafe.Pointer(urlPtr)), 0, 0, 1)
}

func hideChildWindow(cmd *exec.Cmd) {
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
