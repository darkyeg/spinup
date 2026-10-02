package main

import (
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/source"
)

// repoData is the spinup data to apply: the checkout setup recorded, one found nearby, or the built-in copy.
func repoData() source.Source {
	cfg, _ := config.Load()
	return source.Find(cfg.Repo)
}
