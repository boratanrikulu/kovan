package app

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/boratanrikulu/kovan/internal/agent"
	"github.com/boratanrikulu/kovan/internal/config"
)

// driverOf returns the driver for an agent name as a manifest or config holds
// it; "" is claude.
func driverOf(name string) (agent.Driver, error) {
	return agent.For(name)
}

// mustDriver is driverOf for paths that cannot fail: an unknown name falls
// back to claude, which is what such a manifest ran under.
func mustDriver(name string) agent.Driver {
	d, err := driverOf(name)
	if err != nil {
		d, _ = agent.For(agent.Claude)
	}
	return d
}

// agentBin is the binary an agent launches: the config's command for its
// driver, else the name it was given (a bare name or a path).
func agentBin(global *config.Global, name string) string {
	d := mustDriver(name)
	if c := global.Agents[d.Name()].Command; c != "" {
		return c
	}
	if name == "" {
		return d.Name()
	}
	return name
}

// resolveAgentName picks the agent CLI by precedence: the explicit flag, then
// the repo default, then the global default.
func resolveAgentName(flag, repoDefault, globalDefault string) string {
	switch {
	case flag != "":
		return flag
	case repoDefault != "":
		return repoDefault
	case globalDefault != "":
		return globalDefault
	}
	return agent.Claude
}

// agentHome is the CLI's home for an account: the account's own home when it
// sets one, else the CLI's default.
func agentHome(global *config.Global, d agent.Driver, account string) string {
	if acct, ok := global.Accounts[account]; ok && acct.Home != "" {
		return acct.Home
	}
	return d.DefaultHome()
}

// authPrefix is the environment an agent launches with under account: the
// account's extra env, its home when the CLI has a home env, and the token read
// from tokenFile when the CLI takes one.
func authPrefix(global *config.Global, d agent.Driver, account, tokenFile string) string {
	env := map[string]string{}
	for k, v := range accountEnv(global, account) {
		env[k] = v
	}
	if acct, ok := global.Accounts[account]; ok && acct.Home != "" && d.HomeEnv() != "" {
		env[d.HomeEnv()] = acct.Home
	}
	prefix := envPrefix(env)
	if tokenFile != "" && d.TokenEnv() != "" {
		prefix += d.TokenEnv() + "=" + tokenReadExpr(tokenFile) + " "
	}
	return prefix
}

// installed reports whether an agent CLI's binary can be run.
func installed(global *config.Global, name string) bool {
	_, err := exec.LookPath(agentBin(global, name))
	return err == nil
}

// installedAgents lists the known agent CLIs whose binary is on PATH (or at
// its configured path).
func installedAgents(global *config.Global) []string {
	var out []string
	for _, n := range agent.Names() {
		if installed(global, n) {
			out = append(out, n)
		}
	}
	return out
}

// monitorModel is the summarizer model for a driver: its entry in
// monitor.models, else monitor.model for claude (that key names a Claude
// model), else the CLI's own default.
func monitorModel(global *config.Global, d agent.Driver) string {
	if m, ok := global.Monitor.Models[d.Name()]; ok {
		return m
	}
	if d.Name() == agent.Claude {
		return global.Monitor.Model
	}
	return ""
}

// agentLabel is how a picker shows an agent CLI.
func agentLabel(global *config.Global, name string) string {
	if !installed(global, name) {
		return name + " (not installed)"
	}
	return name
}

// accountsFor lists the picker entries for an agent CLI's accounts: the system
// entry first, then the accounts that belong to it. Empty when it has none,
// which hides the field.
func accountsFor(global *config.Global, agentName string) []string {
	d := mustDriver(agentName)
	var names []string
	for name, acct := range global.Accounts {
		if acctAgent(acct) == d.Name() {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return accountPicks(names)
}

// hooksWarning is the warning for an agent whose CLI has no kovan hook in the
// home it runs under, "" when the hook is there. Without the hook no gate
// fires and the board cannot see the agent's state, so this must never pass
// quietly. (Codex also runs a new hook only after it is trusted in /hooks,
// which kovan cannot see.)
func hooksWarning(global *config.Global, d agent.Driver, account string) string {
	path := d.HooksFile(agentHome(global, d, account))
	if hooksPresent(path, d) {
		return ""
	}
	return fmt.Sprintf("%s has no kovan hooks in %s, so its gates are off and the board can't see it; run `kovan setup`", d.Name(), path)
}

// withoutEnv drops key from an environment list.
func withoutEnv(env []string, key string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}
	return out
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
