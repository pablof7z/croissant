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

	if E.OwnerPublicKey == "" {
		S.OwnerPubKey = S.RelaySecretKey.Public()
	} else if pk, ok := pubKeyFromInput(E.OwnerPublicKey); ok {
		S.OwnerPubKey = pk
	} else {
		L.Fatal().Msg("invalid OWNER_PUBLIC_KEY")
	}

	ConfigureGroupCreateRateLimit(S)
}
