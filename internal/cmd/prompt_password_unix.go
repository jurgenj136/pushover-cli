//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris || zos

package cmd

import "golang.org/x/sys/unix"

type systemPasswordTerminal struct{}

func (systemPasswordTerminal) disableEcho(fd int) (func() error, error) {
	state, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return nil, err
	}
	next := *state
	next.Lflag &^= unix.ECHO
	next.Lflag |= unix.ICANON | unix.ISIG
	next.Iflag |= unix.ICRNL
	if err := unix.IoctlSetTermios(fd, ioctlWriteTermios, &next); err != nil {
		return nil, err
	}
	return func() error { return unix.IoctlSetTermios(fd, ioctlWriteTermios, state) }, nil
}

func (systemPasswordTerminal) read(fd int, output []byte) (int, error) {
	return unix.Read(fd, output)
}

func (systemPasswordTerminal) lineTerminator() byte { return '\n' }

func readTerminalPassword(fd, maximum int) ([]byte, error) {
	return readTerminalPasswordWith(fd, maximum, systemPasswordTerminal{})
}
