package config

import (
	"strings"
	"testing"
)

// The update channel is stable unless the owner chose test, in config.json
// or with PROTON_UPDATE_CHANNEL (issue #213).
func TestUpdateChannel(t *testing.T) {
	t.Parallel()
	base := map[string]string{EnvPassword: "secret-for-tests"}
	load := func(saved Settings, env map[string]string) (Config, error) {
		return Load(Chain(FromMap(env), FromMap(saved.Values()), FromMap(base)))
	}
	user := Settings{User: "alice@example.com", CertFile: "c.pem"}
	if cfg, err := load(user, nil); err != nil || cfg.UpdateChannel != ChannelStable {
		t.Errorf("default: %q, %v", cfg.UpdateChannel, err)
	}
	test := user
	test.UpdateChannel = ChannelTest
	if cfg, err := load(test, nil); err != nil || cfg.UpdateChannel != ChannelTest {
		t.Errorf("config.json: %q, %v", cfg.UpdateChannel, err)
	}
	if cfg, err := load(test, map[string]string{EnvUpdateChannel: "Stable"}); err != nil || cfg.UpdateChannel != ChannelStable {
		t.Errorf("environment over config.json: %q, %v", cfg.UpdateChannel, err)
	}
	if _, err := load(user, map[string]string{EnvUpdateChannel: "beta"}); err == nil || !strings.Contains(err.Error(), EnvUpdateChannel) {
		t.Errorf("beta: %v", err)
	}

	v := user.OptionValues()
	v.TestChannel = true
	got, err := ApplyOptions(user, v, map[string]bool{}, false)
	if err != nil || got.UpdateChannel != ChannelTest || !got.OptionValues().TestChannel {
		t.Errorf("switched on: %+v, %v", got, err)
	}
	v.TestChannel = false
	if got, _ = ApplyOptions(got, v, map[string]bool{}, false); got.UpdateChannel != "" {
		t.Errorf("switched off: %q", got.UpdateChannel)
	}
	if got, _ = ApplyOptions(test, v, map[string]bool{EnvUpdateChannel: true}, false); got.UpdateChannel != ChannelTest {
		t.Errorf("locked: %q", got.UpdateChannel)
	}
}
