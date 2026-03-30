package global

import (
	"os"

	"github.com/rs/zerolog"
)

var L = zerolog.New(os.Stderr).Output(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()
