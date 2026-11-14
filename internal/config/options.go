package config

import (
	"os"
	"strconv"
)

// Where a setting's value came from, strongest first.
const (
	FromEnvironment = "environment"
	FromDotEnv      = ".env"
	FromSettings    = "config.json"
	FromDefault     = "default"
)

// Option is one of the owner's options as it is in effect, and where its
// value came from, for `check` and the about tool (issue #158). Never the
// password or anything secret.
type Option struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Origin string `json:"origin" jsonschema:"where the value came from: environment, .env, config.json or default"`
}

// Origin says where key's value comes from: the environment, then .env,
// then the settings file, else the default. It is the order Load reads
// them in (see Chain in cmd/proton-mail-mcp).
func Origin(key string, dotenv map[string]string, saved Settings) string {
	if _, ok := os.LookupEnv(key); ok {
		return FromEnvironment
	}
	if _, ok := dotenv[key]; ok {
		return FromDotEnv
	}
	if _, ok := saved.Values()[key]; ok {
		return FromSettings
	}
	return FromDefault
}

// Options lists the owner's options in cfg with their origins.
func Options(cfg Config, dotenv map[string]string, saved Settings) []Option {
	onOff := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	dir := cfg.AttachmentDir
	if dir == "" {
		dir = "(not found; set it)"
	}
	return []Option{
		{"fetch images from the internet (fetch_image)", onOff(cfg.AllowRemoteImages), Origin(EnvAllowRemoteImages, dotenv, saved)},
		{"attachment folder", dir, Origin(EnvAttachmentDir, dotenv, saved)},
		{"changes per run", strconv.Itoa(cfg.MaxChanges), Origin(EnvMaxChanges, dotenv, saved)},
	}
}
