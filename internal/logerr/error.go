package logerr

import (
	"fmt"
	"runtime"
)

func Fatal(err error) error {
	if err == nil {
		return nil
	}
	stack := make([]byte, 1024)

	len := runtime.Stack(stack, false)

	return fmt.Errorf("%v\n%s", err, string(stack[0:len]))
}

func Error(message string, cause error) error {
	stack := make([]byte, 1024)

	len := runtime.Stack(stack, false)

	if cause != nil {
		return fmt.Errorf("%s: %v\n%s", message, cause, string(stack[0:len]))
	}

	return fmt.Errorf("%s\n%s", message, string(stack[0:len]))
}

func Errorf(format string, args ...interface{}) error {
	stack := make([]byte, 1024)

	len := runtime.Stack(stack, false)

	format = format + "\n%s"
	args = append(args, string(stack[0:len]))

	return fmt.Errorf(format, args...)
}
