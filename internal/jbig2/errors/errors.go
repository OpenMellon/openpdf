// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package errors

import (
	"fmt"

	"golang.org/x/xerrors"
)

func Wrap(err error, processName, message string) error {
	if inner, ok := err.(*processError); ok {
		inner.prefix = ""
	}
	wrapped := newProcessError(message, processName)
	wrapped.cause = err
	return wrapped
}

func (e *processError) Error() string {
	var text string
	if e.prefix != "" {
		text = e.prefix
	}
	text += "Process: " + e.processName
	if e.message != "" {
		text += " Message: " + e.message
	}
	if e.cause != nil {
		text += ". " + e.cause.Error()
	}
	return text
}
func (e *processError) Unwrap() error { return e.cause }

type processError struct {
	prefix      string
	processName string
	message     string
	cause       error
}

func Error(processName, message string) error { return newProcessError(message, processName) }
func newProcessError(message, processName string) *processError {
	return &processError{prefix: "[UniPDF]", message: message, processName: processName}
}

var _ xerrors.Wrapper = (*processError)(nil)

func Errorf(processName, message string, arguments ...any) error {
	return newProcessError(fmt.Sprintf(message, arguments...), processName)
}

func Wrapf(err error, processName, message string, arguments ...any) error {
	if inner, ok := err.(*processError); ok {
		inner.prefix = ""
	}
	wrapped := newProcessError(fmt.Sprintf(message, arguments...), processName)
	wrapped.cause = err
	return wrapped
}
