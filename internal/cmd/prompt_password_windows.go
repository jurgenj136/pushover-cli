package cmd

import "golang.org/x/sys/windows"

type systemPasswordTerminal struct{}

func (systemPasswordTerminal) disableEcho(fd int) (func() error, error) {
	handle := windows.Handle(fd)
	var state uint32
	if err := windows.GetConsoleMode(handle, &state); err != nil {
		return nil, err
	}
	next := state &^ (windows.ENABLE_ECHO_INPUT | windows.ENABLE_LINE_INPUT)
	next |= windows.ENABLE_PROCESSED_INPUT
	if err := windows.SetConsoleMode(handle, next); err != nil {
		return nil, err
	}
	return func() error { return windows.SetConsoleMode(handle, state) }, nil
}

func (systemPasswordTerminal) read(fd int, output []byte) (int, error) {
	var count uint32
	err := windows.ReadFile(windows.Handle(fd), output, &count, nil)
	return int(count), err
}

func (systemPasswordTerminal) lineTerminator() byte { return '\r' }

func readTerminalPassword(fd, maximum int) ([]byte, error) {
	return readTerminalPasswordWith(fd, maximum, systemPasswordTerminal{})
}
