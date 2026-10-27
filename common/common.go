// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package common contains common properties used by the subpackages.
package common

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Info does nothing for dummy logger.
func (DummyLogger) Info(format string, args ...interface{}) {}

// ConsoleLogger is a logger that writes logs to the 'os.Stdout'
type ConsoleLogger struct{ LogLevel LogLevel }

// Trace does nothing for dummy logger.
func (DummyLogger) Trace(format string, args ...interface{}) {}

const releaseMinute = 30
const Version = "5.2.0"

var ReleasedAt = time.Date(releaseYear, releaseMonth, releaseDay, releaseHour, releaseMinute, 0, 0, time.UTC)

// UtcTimeFormat returns a formatted string describing a UTC timestamp.
func UtcTimeFormat(t time.Time) string { return t.Format(utcTimeLayout) + " UTC" }

const (
	LogLevelTrace   LogLevel = 5
	LogLevelDebug   LogLevel = 4
	LogLevelInfo    LogLevel = 3
	LogLevelNotice  LogLevel = 2
	LogLevelWarning LogLevel = 1
	LogLevelError   LogLevel = 0
)

// Debug logs debug message.
func (l WriterLogger) Debug(format string, args ...interface{}) {
	if l.LogLevel >= LogLevelDebug {
		prefix := "[DEBUG] "
		l.logToWriter(l.Output, prefix, format, args...)
	}
}

// Warning logs warning message.
func (l ConsoleLogger) Warning(format string, args ...interface{}) {
	if l.LogLevel >= LogLevelWarning {
		prefix := "[WARNING] "
		l.output(os.Stdout, prefix, format, args...)
	}
}

// Trace logs trace message.
func (l ConsoleLogger) Trace(format string, args ...interface{}) {
	if l.LogLevel >= LogLevelTrace {
		prefix := "[TRACE] "
		l.output(os.Stdout, prefix, format, args...)
	}
}

// Notice does nothing for dummy logger.
func (DummyLogger) Notice(format string, args ...interface{}) {}

// DummyLogger does nothing.
type DummyLogger struct{}

// IsLogLevel returns true if log level is greater or equal than `level`.
// Can be used to avoid resource intensive calls to loggers.
func (l WriterLogger) IsLogLevel(level LogLevel) bool { return l.LogLevel >= level }

// Error logs error message.
func (l WriterLogger) Error(format string, args ...interface{}) {
	if l.LogLevel >= LogLevelError {
		prefix := "[ERROR] "
		l.logToWriter(l.Output, prefix, format, args...)
	}
}

const releaseYear = 2026

// IsLogLevel returns false from dummy logger. A logger that emits nothing
// reports no level enabled so the standard "guard before log" idiom short-circuits.
func (DummyLogger) IsLogLevel(level LogLevel) bool { return false }

const releaseMonth = 10

func (l ConsoleLogger) output(w io.Writer, prefix string, format string, args ...interface{}) {
	writeLog(w, prefix, format, args...)
}

// LogLevel is the verbosity level for logging.
type LogLevel int

func (l WriterLogger) logToWriter(w io.Writer, prefix string, format string, args ...interface{}) {
	writeLog(w, prefix, format, args)
}

// Info logs info message.
func (l WriterLogger) Info(format string, args ...interface{}) {
	if l.LogLevel >= LogLevelInfo {
		prefix := "[INFO] "
		l.logToWriter(l.Output, prefix, format, args...)
	}
}

// Logger is the interface used for logging in the openpdf package.
type Logger interface {
	Error(format string, args ...interface{})
	Warning(format string, args ...interface{})
	Notice(format string, args ...interface{})
	Info(format string, args ...interface{})
	Debug(format string, args ...interface{})
	Trace(format string, args ...interface{})
	IsLogLevel(level LogLevel) bool
}

const utcTimeLayout = "2 January 2006 at 15:04"

// Trace logs trace message.
func (l WriterLogger) Trace(format string, args ...interface{}) {
	if l.LogLevel >= LogLevelTrace {
		prefix := "[TRACE] "
		l.logToWriter(l.Output, prefix, format, args...)
	}
}

// Warning does nothing for dummy logger.
func (DummyLogger) Warning(format string, args ...interface{}) {}

// WriterLogger is the logger that writes data to the Output writer
type WriterLogger struct {
	LogLevel LogLevel
	Output   io.Writer
}

// IsLogLevel returns true if log level is greater or equal than `level`.
// Can be used to avoid resource intensive calls to loggers.
func (l ConsoleLogger) IsLogLevel(level LogLevel) bool { return l.LogLevel >= level }
func writeLog(w io.Writer, prefix string, format string, args ...interface{}) {
	_, file, line, ok := runtime.Caller(3)
	if !ok {
		file = "???"
		line = 0
	} else {
		file = filepath.Base(file)
	}
	msg := fmt.Sprintf("%s %s:%d ", prefix, file, line) + format + "\n"
	fmt.Fprintf(w, msg, args...)
}

// Error logs error message.
func (l ConsoleLogger) Error(format string, args ...interface{}) {
	if l.LogLevel >= LogLevelError {
		prefix := "[ERROR] "
		l.output(os.Stdout, prefix, format, args...)
	}
}

// Notice logs notice message.
func (l WriterLogger) Notice(format string, args ...interface{}) {
	if l.LogLevel >= LogLevelNotice {
		prefix := "[NOTICE] "
		l.logToWriter(l.Output, prefix, format, args...)
	}
}

var Log Logger = DummyLogger{}

// Error does nothing for dummy logger.
func (DummyLogger) Error(format string, args ...interface{}) {}

const releaseDay = 18

// Info logs info message.
func (l ConsoleLogger) Info(format string, args ...interface{}) {
	if l.LogLevel >= LogLevelInfo {
		prefix := "[INFO] "
		l.output(os.Stdout, prefix, format, args...)
	}
}

// NewWriterLogger creates new 'writer' logger.
func NewWriterLogger(logLevel LogLevel, writer io.Writer) *WriterLogger {
	l := WriterLogger{Output: writer, LogLevel: logLevel}
	return &l
}

// NewConsoleLogger creates new console logger.
func NewConsoleLogger(logLevel LogLevel) *ConsoleLogger { return &ConsoleLogger{LogLevel: logLevel} }

// Warning logs warning message.
func (l WriterLogger) Warning(format string, args ...interface{}) {
	if l.LogLevel >= LogLevelWarning {
		prefix := "[WARNING] "
		l.logToWriter(l.Output, prefix, format, args...)
	}
}

const releaseHour = 15

// Debug logs debug message.
func (l ConsoleLogger) Debug(format string, args ...interface{}) {
	if l.LogLevel >= LogLevelDebug {
		prefix := "[DEBUG] "
		l.output(os.Stdout, prefix, format, args...)
	}
}

// Notice logs notice message.
func (l ConsoleLogger) Notice(format string, args ...interface{}) {
	if l.LogLevel >= LogLevelNotice {
		prefix := "[NOTICE] "
		l.output(os.Stdout, prefix, format, args...)
	}
}

// Debug does nothing for dummy logger.
func (DummyLogger) Debug(format string, args ...interface{}) {}

// SetLogger sets 'logger' to be used by the OpenPDF library.
func SetLogger(logger Logger) { Log = logger }
