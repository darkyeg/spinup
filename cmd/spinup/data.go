package main

import (
	"path/filepath"

	"github.com/darkyeg/spinup/internal/agentconfig"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/library"
	"github.com/darkyeg/spinup/internal/source"
)

// oldPrivateRepo is where earlier spinup versions kept your own files: a git repo inside the checkout.
const oldPrivateRepo = "local"

// repoData is spinup's own data: the checkout setup recorded, one found nearby, or the built-in copy.
func repoData() source.Source {
	cfg, _ := config.Load()
	return source.Find(cfg.Repo)
}

// yourLibrary is your skills list, own skills and instructions; the first time, it takes in the private
// repo of earlier spinup versions.
func yourLibrary(repo source.Source) library.Library {
	lib := library.Here()
	dir, err := repo.Checkout()
	if err != nil {
		return lib
	}
	adopted, err := lib.Adopt(filepath.Join(dir, oldPrivateRepo))
	if err != nil {
		step("Couldn't copy your old private files into %s: %v", lib.Dir(), err)
	} else if len(adopted) > 0 {
		step("Copied your old private files into %s: %v (the old folder can go)", lib.Dir(), adopted)
	}
	return lib
}

func agentSources(repo source.Source) agentconfig.Sources {
	return agentconfig.Sources{Spinup: repo, Yours: yourLibrary(repo)}
}
