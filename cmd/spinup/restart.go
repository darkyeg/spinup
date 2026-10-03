package main

import "github.com/darkyeg/spinup/internal/config"

// bootState is whether the accounts service is registered to start at boot and answers.
type bootState int

const (
	notRegistered bootState = iota
	registeredSilent
	answering
)

type serviceFacts struct {
	boot                         bootState
	binaryChanged, configChanged bool
}

// restartReason says why setup must restart the service, which hands the accounts away and back;
// empty leaves a running, unchanged service alone.
func restartReason(f serviceFacts) string {
	switch {
	case f.boot == notRegistered:
		return "it isn't registered to start at boot"
	case f.boot == registeredSilent:
		return "it isn't answering"
	case f.binaryChanged:
		return "this spinup differs from the installed one"
	case f.configChanged:
		return "its settings changed"
	}
	return ""
}

func currentFacts(cfg config.Config, settingsChanged bool) serviceFacts {
	return serviceFacts{
		boot:          bootStateOf(cfg),
		binaryChanged: binaryChanged(),
		configChanged: settingsChanged,
	}
}

func bootStateOf(cfg config.Config) bootState {
	if !autostartRegistered(cfg.Hold) {
		return notRegistered
	}
	if _, err := localService(cfg, "").leader(); err != nil {
		return registeredSilent
	}
	return answering
}

// settings are what a running service starts from: config.json and the keys.
type settings struct {
	cfg     config.Config
	secrets config.Secrets
}

// storedSettings is the zero value when nothing valid is stored, which counts as changed.
func storedSettings() settings {
	cfg, err := config.Load()
	if err != nil {
		return settings{}
	}
	secrets, _ := config.LoadSecrets(cfg)
	return settings{cfg, secrets}
}
