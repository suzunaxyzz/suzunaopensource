//go:build !windows

package main

import "os/exec"

func attachConsole() bool                       { return false }
func allocConsole() bool                        { return false }
func showErrorMessageBox(title, message string) {}
func showInfoMessageBox(title, message string)  {}
func openURL(url string)                        {}
func hideChildWindow(cmd *exec.Cmd)             {}
