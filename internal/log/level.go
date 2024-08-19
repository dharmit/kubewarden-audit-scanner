package log

import (
	"fmt"

	"github.com/rs/zerolog"
)

// Level implements the Value interface (https://pkg.go.dev/github.com/spf13/pflag@v1.0.5#Value).
// Therefore we can get this value from a flag, and show an error if a supported value is not provided.
type Level struct {
	value string
}

func GetSupportedValues() [6]string {
	return [6]string{zerolog.LevelTraceValue, zerolog.LevelDebugValue, zerolog.LevelInfoValue, zerolog.LevelWarnValue, zerolog.LevelErrorValue, zerolog.LevelFatalValue}
}

func (l *Level) SetZeroLogLevel() {
	level, err := zerolog.ParseLevel(l.String())
	if err != nil {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	} else {
		zerolog.SetGlobalLevel(level)
	}
}

func (l *Level) String() string {
	if l.value == "" {
		return "info"
	}
	return l.value
}

func (l *Level) Set(level string) error {
	supportedValues := GetSupportedValues()
	isIncluded := false
	for _, opt := range supportedValues {
		if level == opt {
			l.value = level
			isIncluded = true
		}
	}

	if !isIncluded {
		return fmt.Errorf("supported values: %s", supportedValues)
	}

	return nil
}

func (l *Level) Type() string {
	return "string"
}
