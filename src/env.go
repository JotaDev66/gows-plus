package main

import (
	"github.com/caarlos0/env/v11"
)

type ClientConfig struct {
	BrowserName string `env:"WAHA_CLIENT_BROWSER_NAME" envDefault:"Firefox"`
	DeviceName  string `env:"WAHA_CLIENT_DEVICE_NAME"  envDefault:"Ubuntu"`
}

func getClientConfig() ClientConfig {
	cfg := ClientConfig{}
	if err := env.Parse(&cfg); err != nil {
		panic(err)
	}
	return cfg
}
