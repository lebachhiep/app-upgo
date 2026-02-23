package sentryhook

import (
	"github.com/getsentry/sentry-go"
	"github.com/rs/zerolog"
)

// Hook sends zerolog error/fatal events to Sentry.
type Hook struct{}

func (h Hook) Run(e *zerolog.Event, level zerolog.Level, msg string) {
	if level < zerolog.ErrorLevel {
		return
	}

	event := sentry.NewEvent()
	event.Message = msg
	event.Level = mapLevel(level)
	sentry.CaptureEvent(event)
}

func mapLevel(l zerolog.Level) sentry.Level {
	switch l {
	case zerolog.FatalLevel, zerolog.PanicLevel:
		return sentry.LevelFatal
	default:
		return sentry.LevelError
	}
}
