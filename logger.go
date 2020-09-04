package service_start

import "log"

type logger func(format string, v ...interface{})

func (l logger) Info(format string, v ...interface{}) {
	l(format, v...)
}

func StandardLogger() Logger {
	return logger(log.Printf)
}

func NopLogger() Logger {
	l := func(format string, v ...interface{}) {}
	return logger(l)
}
