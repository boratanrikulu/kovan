package app

import (
	"fmt"
	"os"

	"github.com/boratanrikulu/kovan/internal/config"
)

const oauthEnvKey = "CLAUDE_CODE_OAUTH_TOKEN"

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

// launchCommand builds the agent command, prefixing the OAuth token env read
// from tokenFile when an account is in play.
func launchCommand(agent, prompt string, mode launchMode, tokenFile, addDir, sessionID string) string {
	cmd := agentCommand(agent, prompt, mode, addDir, sessionID)
	if tokenFile == "" {
		return cmd
	}
	return oauthEnvKey + "=" + tokenReadExpr(tokenFile) + " " + cmd
}
