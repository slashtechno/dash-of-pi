package main

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

type Logger struct {
	verbose bool
	mu      sync.Mutex
	logger  *log.Logger
}

func NewLogger(verbose bool) *Logger {
	return &Logger{
		verbose: verbose,
		logger:  log.New(os.Stdout, "", 0),
	}
}

func (l *Logger) Printf(format string, v ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	msg := fmt.Sprintf("[%s] %s", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, v...))
	l.logger.Println(msg)
}

func (l *Logger) Debugf(format string, v ...interface{}) {
	// Debug logging disabled in production
}

func (l *Logger) Fatalf(format string, v ...interface{}) {
	l.Printf("[FATAL] "+format, v...)
	os.Exit(1)
}
