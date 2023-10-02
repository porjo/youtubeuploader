package utils

import "log"

type Logger struct {
	debug bool
}

func NewLogger(debug bool) Logger {
	return Logger{debug: debug}
}

func (l *Logger) Debugf(format string, args ...interface{}) {
	if l.debug {
		log.Printf("[DEBUG] "+format, args...)
	}
}
