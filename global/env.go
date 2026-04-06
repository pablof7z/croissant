package global

type Env struct {
	Port           string `envconfig:"PORT" default:"9888"`
	Host           string `envconfig:"HOST" default:"127.0.0.1"`
	Domain         string `envconfig:"DOMAIN"`
	DataPath       string `envconfig:"DATAPATH" default:"data"`
	OwnerPublicKey string `envconfig:"OWNER_PUBLIC_KEY"`
}

var E Env
