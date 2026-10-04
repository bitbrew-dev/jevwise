package config

import (
	"github.com/phuslu/log"
	"github.com/spf13/viper"
)

func LoadConfig() {
	log.Debug().Msg("loading configs...")

	viper.AddConfigPath("")
}
