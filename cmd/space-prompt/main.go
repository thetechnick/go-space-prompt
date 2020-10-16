/*
Copyright 2020 The Go-Spaceship Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"sigs.k8s.io/yaml"
)

type Context struct {
	InSSH        bool
	Duration     time.Duration
	Status, Jobs int
	Home         string
}

func main() {
	// Init
	ctx := &Context{
		InSSH: os.Getenv("SSH_CONNECTION") != "",
	}
	var (
		durationStr string
	)
	flag.StringVar(&durationStr, "duration", "", "duration of the last command")
	flag.IntVar(&ctx.Status, "status", 0, "status of last command")
	flag.IntVar(&ctx.Jobs, "jobs", 0, "number of background jobs")
	flag.Parse()
	if durationStr != "" {
		i, _ := strconv.Atoi(durationStr)
		ctx.Duration = time.Duration(int64(i))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	ctx.Home = home

	color := os.Getenv("SPACE_PROMPT_COLOR")
	if color == "" {
		color = "blue"
	}

	// Run Modules
	var (
		user       = &UserModule{}
		kubernetes = &KubernetesModule{}
		directory  = &DirectoryModule{}
		git        = &GitModule{}
		golang     = &GolangModule{}
		hostname   = &HostnameModule{}
		status     = &StatusModule{}
		took       = &TookModule{}
	)
	modules := []module{
		user, kubernetes, directory,
		git, golang, hostname, status,
		took,
	}
	var wg sync.WaitGroup
	wg.Add(len(modules))
	for _, m := range modules {
		go func(m module) {
			must(m.Init(ctx))
			wg.Done()
		}(m)
	}
	wg.Wait()

	// Build
	fmt.Print("\n" +
		user.Output() + kubernetes.Output() + directory.Output() +
		git.Output() + golang.Output() + took.Output() + "\n" +
		hostname.Output() + status.Output() + "%K{" + color + "}%F{black} %f%k%F{" + color + "} %f")
}

type module interface {
	Init(ctx *Context) error
	Output() string
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

// ----------
// Kubernetes
// ----------

type KubernetesModule struct {
	output string
}
type kubeconfig struct {
	CurrentContext string `json:"current-context"`
}

func (m *KubernetesModule) Init(ctx *Context) error {
	kubeconfigFile, err := ioutil.ReadFile(
		path.Join(ctx.Home, ".kube", "config"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading kubeconfig: %w", err)
	}

	kc := &kubeconfig{}
	if err := yaml.Unmarshal(kubeconfigFile, kc); err != nil {
		return fmt.Errorf("unmarshal yaml: %w", err)
	}
	if kc.CurrentContext == "" {
		return nil
	}

	m.output = "%B%F{blue} ☸ " + kc.CurrentContext + "%b%f"
	return nil
}

func (m *KubernetesModule) Output() string {
	return m.output
}

// ---------
// Directory
// ---------
type DirectoryModule struct {
	output string
}

func (m *DirectoryModule) Init(ctx *Context) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	var dir string
	if wd == ctx.Home {
		dir = "~"
	} else {
		dir = path.Base(wd)
	}

	m.output = `%F{white} in%f %F{cyan}%B` + dir + `%b%f`
	return nil
}

func (m *DirectoryModule) Output() string {
	return m.output
}

// --------
// Hostname
// --------
type HostnameModule struct {
	output string
}

func (m *HostnameModule) Init(ctx *Context) error {
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("getting hostname: %w", err)
	}

	idx := strings.Index(hostname, ".")
	if idx != -1 {
		hostname = hostname[:idx]
	}

	if ctx.InSSH {
		m.output += "%K{black} ﴽ%k"
	}
	m.output += "%K{black}%F{white} " + hostname + "%k%f"
	return nil
}

func (m *HostnameModule) Output() string {
	return m.output
}

// ------
// Golang
// ------
type GolangModule struct {
	output string
}

func (m *GolangModule) Init(ctx *Context) error {
	_, err := os.Stat("go.mod")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking go.mod: %w", err)
	}

	version, err := exec.Command("go", "version").Output()
	if err != nil {
		return nil
	}
	parts := strings.Split(string(version), " ")
	if len(parts) < 3 {
		return nil
	}
	goVersion := parts[2][2:]
	if strings.HasPrefix(goVersion, "devel") {
		goVersion = parts[3]
	}

	m.output += "%F{cyan} Go v" + goVersion + "%f"
	return nil
}

func (m *GolangModule) Output() string {
	return m.output
}

// ------
// Status
// ------
type StatusModule struct {
	output string
}

func (m *StatusModule) Init(ctx *Context) error {
	if ctx.Status == 0 {
		m.output = "%B%K{black}%F{green} ✓ %k%f%b"
		return nil
	}
	m.output = "%B%K{black}%F{red} ✗ %k%f%b"
	return nil
}

func (m *StatusModule) Output() string {
	return m.output
}

// ----
// Took
// ----
type TookModule struct {
	output string
}

func (m *TookModule) Init(ctx *Context) error {
	if ctx.Duration < time.Second*2 {
		return nil
	}

	m.output = ` took %B%F{yellow}` + ctx.Duration.Round(time.Millisecond).String() + `%b%f`
	return nil
}

func (m *TookModule) Output() string {
	return m.output
}

// ----
// User
// ----
type UserModule struct {
	output string
}

func (m *UserModule) Init(ctx *Context) error {
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("get current user: %w", err)
	}
	if u == nil {
		return nil
	}

	if u.Username != "root" {
		return nil
	}

	m.output = `%F{red} ` + u.Username + `%f`
	return nil
}

func (m *UserModule) Output() string {
	return m.output
}

// ---
// Git
// ---

var (
	stagedRegEx   = regexp.MustCompile(`^(A[ MDAU] |M[ MD] |UA)`)
	modifiedRegEx = regexp.MustCompile(`^[ MARC]M `)
	renamedRegEx  = regexp.MustCompile(`^R[ MD]`)
	deletedRegEx  = regexp.MustCompile(`^([MARCDU ]D|D[ UM]) `)
	unmergedRegEx = regexp.MustCompile(`^(U[UDA]|AA|DD|[DA]U) `)
	aheadRegEx    = regexp.MustCompile(`^## .*ahead`)
	behindRegEx   = regexp.MustCompile(`^## .*behind`)
)

const (
	GitUntracked = "?"
	GitAdded     = "+"
	GitModified  = "!"
	GitRenamed   = "»"
	GitDeleted   = "✘"
	GitStashed   = "$"
	GitUnmerged  = "="
	GitAhead     = "⇡"
	GitBehind    = "⇣"
	GitDiverged  = "⇕"
)

type GitModule struct {
	output string
}

func (m *GitModule) Init(ctx *Context) error {
	output, err := exec.Command("git", "status", "--porcelain", "-b").Output()
	if err != nil {
		// no git?
		return nil
	}

	stash := exec.Command("git", "rev-parse", "--verify", "refs/stash").Run() == nil

	// branch
	if len(output) < 4 {
		return nil
	}
	branchEndIndex := bytes.IndexAny(output, ".\n")
	branch := string(output[3:branchEndIndex])
	branch = strings.TrimPrefix(branch, "No commits yet on ")

	var status string
	// untracked files
	if bytes.Contains(output, []byte("\n??")) {
		status += GitUntracked
	}

	// staged
	if stagedRegEx.Match(output) {
		status += GitAdded
	}

	// modified
	if modifiedRegEx.Match(output) {
		status += GitModified
	}

	// renamed
	if renamedRegEx.Match(output) {
		status += GitRenamed
	}

	// deleted
	if deletedRegEx.Match(output) {
		status += GitDeleted
	}

	if stash {
		status += GitStashed
	}

	// unmerged
	if unmergedRegEx.Match(output) {
		status += GitUnmerged
	}

	var (
		isAhead  = aheadRegEx.Match(output)
		isBehind = behindRegEx.Match(output)
	)
	if isAhead && isBehind {
		status += GitDiverged
	} else if isAhead {
		status += GitAhead
	} else if isBehind {
		status += GitBehind
	}

	m.output = `%F{white} on%f%F{magenta}%B  ` + branch + `%b%f`
	if status != "" {
		m.output += ` %F{red}[` + status + `]%f`
	}
	return nil
}

func (m *GitModule) Output() string {
	return m.output
}
