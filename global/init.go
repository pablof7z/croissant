package global

import "github.com/kelseyhightower/envconfig"

func Init() {
	err := envconfig.Process("", &E)
	if err != nil {
		L.Fatal().Err(err).Msg("error loading environment configuration")
	}

	S, err = loadSettings(E.DataPath)
	if err != nil {
		L.Fatal().Err(err).Msg("failed to load settings")
	}

	ConfigureGroupCreateRateLimit(S)
}
