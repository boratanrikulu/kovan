package app

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/boratanrikulu/kovan/internal/config"
)

const oauthEnvKey = "CLAUDE_CODE_OAUTH_TOKEN"

// systemAccount is the picker entry for injecting no token at all: the agent
// runs under whatever account the machine is logged into. An injected token
// authenticates but carries none of the account's entitlements, so an agent
// that wants them (1M context on Max/Team) has to take the login this way.
const systemAccount = "default (system)"

// accountValue maps a picker entry to the name stored in the manifest. The
// system entry stores "", which is what skips token injection.
func accountValue(pick string) string {
	if pick == systemAccount {
		return ""
	}
	return pick
}

// accountPicks lists the pickable accounts: the system entry first, then the
// configured ones. Empty when none are configured, which hides the field.
func accountPicks(configured []string) []string {
	if len(configured) == 0 {
		return nil
	}
	return append([]string{systemAccount}, configured...)
}

// resolveAccount picks the account by precedence: the explicit flag, then the
// repo default, then the global default, then "" (the logged-in account).
func resolveAccount(flag, repoDefault, globalDefault string) string {
	switch {
	case flag != "":
		return flag
	case repoDefault != "":
		return repoDefault
	default:
		return globalDefault
	}
}

// accountTokenFile resolves an account name to its validated token-file path. An
// empty account yields "" (the logged-in account, no token injected). It checks
// the file exists and is not group/world-readable, but never reads the token —
// the launch command cats it at runtime, so the token stays out of kovan.
func accountTokenFile(global *config.Global, account string) (string, error) {
	if account == "" {
		return "", nil
	}
	acct, ok := global.Accounts[account]
	if !ok {
		return "", fmt.Errorf("account %q: not configured in ~/.kovan/config.yaml", account)
	}
	info, err := os.Stat(acct.TokenFile)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("account %q: token file not found at %s; run 'claude setup-token' and save it there", account, acct.TokenFile)
	}
	if err != nil {
		return "", fmt.Errorf("account %q: %w", account, err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("account %q: token file %s is group/world-readable; chmod 600 it", account, acct.TokenFile)
	}
	return acct.TokenFile, nil
}

// tokenReadExpr is the shell expression that reads a token file at launch. Only
// the path appears in argv; the token value never does.
func tokenReadExpr(tokenFile string) string {
	return `"$(cat ` + shellQuote(tokenFile) + `)"`
}

// accountEnv is the extra environment an account's agents launch with. An
// injected token authenticates but carries none of the account's entitlements,
// so this is where a config compensates — pinning the Opus model with a [1m]
// suffix, for instance, to reach the extended context window the picker refuses.
func accountEnv(global *config.Global, account string) map[string]string {
	if account == "" {
		return nil
	}
	return global.Accounts[account].Env
}

// envPrefix renders extra environment as a command prefix, sorted so the launch
// command is stable across runs.
func envPrefix(env map[string]string) string {
	if len(env) == 0 {
		return ""
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=" + shellQuote(env[k]) + " ")
	}
	return b.String()
}

// launchCommand builds the agent command, prefixing the account's extra
// environment and the OAuth token env read from tokenFile when one is in play.
func launchCommand(agent, prompt string, mode launchMode, tokenFile string, env map[string]string, addDir, sessionID string) string {
	cmd := agentCommand(agent, prompt, mode, addDir, sessionID)
	if tokenFile != "" {
		cmd = oauthEnvKey + "=" + tokenReadExpr(tokenFile) + " " + cmd
	}
	return envPrefix(env) + cmd
}
