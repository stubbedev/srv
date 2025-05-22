// Package shell provides helpers for executing shell commands.
package shell

import (
	"os"
	"os/exec"
	"strings"
)

// Run executes a command with stdout/stderr attached.
func Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunQuiet executes a command and returns its output.
func RunQuiet(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// RunWithStdin executes a command with the given stdin content.
func RunWithStdin(stdin string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Sudo executes a command with sudo, stdout/stderr attached.
func Sudo(args ...string) error {
	return Run("sudo", args...)
}

// SudoWrite writes content to a file using sudo tee.
func SudoWrite(path, content string) error {
	return RunWithStdin(content, "sudo", "tee", path)
}

// SudoMkdir creates a directory with sudo.
func SudoMkdir(path string) error {
	return Sudo("mkdir", "-p", path)
}

// SudoRemove removes a file with sudo.
func SudoRemove(path string) error {
	return Sudo("rm", "-f", path)
}

// SudoSystemctl runs a systemctl command with sudo.
func SudoSystemctl(action, service string) error {
	return Sudo("systemctl", action, service)
}

// Exists checks if a command exists in PATH.
func Exists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Mkcert runs mkcert with the given arguments.
func Mkcert(args ...string) error {
	return Run("mkcert", args...)
}

// MkcertQuiet runs mkcert and returns its output.
func MkcertQuiet(args ...string) ([]byte, error) {
	return RunQuiet("mkcert", args...)
}

// Dig runs dig and returns the output.
func Dig(args ...string) (string, error) {
	output, err := RunQuiet("dig", args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// CheckPort checks if a port is in use using ss or netstat.
func CheckPort(port string) (bool, error) {
	var output []byte
	var err error

	if Exists("ss") {
		output, err = RunQuiet("ss", "-tuln")
	} else if Exists("netstat") {
		output, err = RunQuiet("netstat", "-tuln")
	} else {
		return false, nil // Can't check, assume available
	}

	if err != nil {
		return false, err
	}

	return strings.Contains(string(output), ":"+port+" "), nil
}
