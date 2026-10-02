package project

import (
	"context"
	"regexp"
	"strings"
)

var (
	githubURL = regexp.MustCompile(`^(git@github\.com:|https://github\.com/|ssh://git@github\.com/)`)
	scpURL    = regexp.MustCompile(`^(?:[\w.-]+@)?([\w.-]+):([\w.-]+/[\w.-]+?)(?:\.git)?/?$`)
)

// Remote is the origin remote; Rewrite is the github.com URL it should have, when it differs.
type Remote struct {
	URL     string
	Rewrite string
	Problem string
}

func scpTarget(url string) (host, repoPath string, ok bool) {
	match := scpURL.FindStringSubmatch(url)
	if match == nil {
		return "", "", false
	}
	return match[1], match[2], true
}

func githubSSHURL(repoPath string) string { return "git@github.com:" + repoPath + ".git" }

func resolvesToGitHub(sshConfig string) bool {
	for _, line := range strings.Split(strings.ToLower(sshConfig), "\n") {
		if strings.TrimSpace(line) == "hostname github.com" {
			return true
		}
	}
	return false
}

func inspectRemote(ctx context.Context, run Runner, root string) Remote {
	out, err := run.Output(ctx, "git", "-C", root, "remote", "get-url", "origin")
	if err != nil {
		return Remote{Problem: err.Error()}
	}
	url := strings.TrimSpace(out)
	if githubURL.MatchString(url) {
		return Remote{URL: url}
	}
	host, repoPath, ok := scpTarget(url)
	if !ok {
		return Remote{URL: url}
	}
	sshConfig, _ := run.Output(ctx, "ssh", "-G", host)
	if resolvesToGitHub(sshConfig) {
		return Remote{URL: url, Rewrite: githubSSHURL(repoPath)}
	}
	return Remote{URL: url}
}

func rewriteRemote(run Runner, root string, remote Remote) func(context.Context) (string, error) {
	setURL := func(ctx context.Context, url string) error {
		_, err := run.Output(ctx, "git", "-C", root, "remote", "set-url", "origin", url)
		return err
	}
	return func(ctx context.Context) (string, error) {
		if err := setURL(ctx, remote.Rewrite); err != nil {
			return "", err
		}
		if _, err := run.Output(ctx, "git", "-C", root, "ls-remote", "origin", "HEAD"); err != nil {
			if err := setURL(ctx, remote.URL); err != nil {
				return "", err
			}
			return "Kept " + remote.URL + ": github.com didn't accept your SSH key", nil
		}
		return "Remote is now " + remote.Rewrite, nil
	}
}
