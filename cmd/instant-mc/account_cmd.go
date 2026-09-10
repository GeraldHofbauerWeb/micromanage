package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/GeraldHofbauerWeb/instant-launcher/internal/auth"
	"github.com/GeraldHofbauerWeb/instant-launcher/internal/instance"
	"github.com/GeraldHofbauerWeb/instant-launcher/internal/launcher"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(accountCmd)
	accountCmd.AddCommand(accountListCmd, accountLoginCmd, accountOfflineCmd,
		accountUseCmd, accountRemoveCmd)
}

var accountCmd = &cobra.Command{
	Use:   "account",
	Short: "Manage the accounts the game launches as",
	Long: `Manage the accounts the game launches as.

Accounts are shared with the graphical launcher: signing in here is enough for
both. A Microsoft account is needed for servers running in online mode; a local
account plays single-player in full.`,
}

var accountListCmd = &cobra.Command{
	Use:           "list",
	Short:         "List the signed-in accounts",
	Args:          cobra.NoArgs,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := openAccountStore()
		if err != nil {
			return err
		}

		accounts, active := store.List()
		if len(accounts) == 0 {
			fmt.Println("No accounts yet. Run 'account login' or 'account offline <name>'.")
			return nil
		}

		for _, a := range accounts {
			marker := " "
			if a.UUID == active {
				marker = "*"
			}
			state := string(a.Kind)
			switch {
			case a.NeedsReauth:
				state = "msa, sign in again"
			case a.Kind == auth.KindMSA && !a.Usable():
				state = "msa, expired (renewed at launch)"
			}
			fmt.Printf("%s %-16s %-32s %s\n", marker, a.Name, a.UUID, state)
		}
		return nil
	},
}

var accountLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Sign in with a Microsoft account",
	Long: `Sign in with a Microsoft account.

The launcher prints a code and a link; enter the code there and this command
finishes once the browser part is done. The account is stored for both the CLI
and the graphical launcher.`,
	Args:          cobra.NoArgs,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := newManager()
		if err != nil {
			return err
		}
		store, err := launcher.NewAccountStore(manager.AppDir)
		if err != nil {
			return err
		}

		client := auth.NewMSA(msaClientID(manager))
		if !client.Configured() {
			return fmt.Errorf("%w; set it with 'config msa-client-id <id>' "+
				"or the INSTANT_LAUNCHER_MSA_CLIENT_ID environment variable", auth.ErrNotConfigured)
		}
		client.Observer = func(step string) { fmt.Fprintf(os.Stderr, "  %s\n", step) }

		code, err := client.StartDeviceCode(cmd.Context())
		if err != nil {
			return err
		}

		where := code.VerificationURI
		if where == "" {
			where = "https://microsoft.com/link"
		}
		fmt.Fprintf(os.Stderr, "\n  Open %s and enter the code:\n\n      %s\n\n  Waiting…\n\n",
			where, code.UserCode)

		tokens, err := client.WaitForToken(cmd.Context(), code)
		if err != nil {
			return err
		}
		account, err := client.SignIn(cmd.Context(), tokens)
		if err != nil {
			return err
		}
		if err := store.Add(account); err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "\nSigned in as %s\n", account.Name)
		return nil
	},
}

var accountOfflineCmd = &cobra.Command{
	Use:           "offline <name>",
	Short:         "Add a local account",
	Args:          cobra.ExactArgs(1),
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := openAccountStore()
		if err != nil {
			return err
		}

		account, err := auth.NewOfflineAccount(args[0])
		if err != nil {
			return err
		}
		if err := store.Add(account); err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "Playing as %s (local account)\n", account.Name)
		return nil
	},
}

var accountUseCmd = &cobra.Command{
	Use:           "use <name-or-id>",
	Short:         "Choose which account launches",
	Args:          cobra.ExactArgs(1),
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := openAccountStore()
		if err != nil {
			return err
		}

		account, err := findAccount(store, args[0])
		if err != nil {
			return err
		}
		if err := store.SetActive(account.UUID); err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "Playing as %s\n", account.Name)
		return nil
	},
}

var accountRemoveCmd = &cobra.Command{
	Use:           "remove <name-or-id>",
	Short:         "Forget an account and its stored tokens",
	Args:          cobra.ExactArgs(1),
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := openAccountStore()
		if err != nil {
			return err
		}

		account, err := findAccount(store, args[0])
		if err != nil {
			return err
		}
		if err := store.Remove(account.UUID); err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "Removed %s\n", account.Name)
		return nil
	},
}

// openAccountStore loads the accounts shared with the graphical launcher.
func openAccountStore() (*launcher.AccountStore, error) {
	manager, err := newManager()
	if err != nil {
		return nil, err
	}
	return launcher.NewAccountStore(manager.AppDir)
}

// findAccount resolves a name or uuid to one account, refusing an ambiguous
// name rather than guessing which of two players was meant.
func findAccount(store *launcher.AccountStore, wanted string) (auth.Account, error) {
	accounts, _ := store.List()

	var matches []auth.Account
	for _, a := range accounts {
		if a.UUID == wanted || strings.EqualFold(a.Name, wanted) {
			matches = append(matches, a)
		}
	}

	switch len(matches) {
	case 0:
		return auth.Account{}, fmt.Errorf("no account called %q; 'account list' shows them", wanted)
	case 1:
		return matches[0], nil
	default:
		return auth.Account{}, fmt.Errorf("%q matches %d accounts; use the id instead", wanted, len(matches))
	}
}

// msaClientID resolves the Azure application id the same way the graphical
// launcher does: environment first, then the saved configuration, then the id
// this binary was built with.
func msaClientID(manager *instance.Manager) string {
	if id := msaClientIDFromEnv(); id != "" {
		return id
	}
	if id := strings.TrimSpace(manager.MSAClientID); id != "" {
		return id
	}
	return strings.TrimSpace(defaultMSAClientID)
}

// msaClientIDFromEnv reads the application id from the environment. The old
// name is still accepted: it was the launcher's own before the rename, and
// breaking someone's shell profile over a rename would be rude.
func msaClientIDFromEnv() string {
	for _, key := range []string{"INSTANT_LAUNCHER_MSA_CLIENT_ID", "MIM_MSA_CLIENT_ID"} {
		if id := strings.TrimSpace(os.Getenv(key)); id != "" {
			return id
		}
	}
	return ""
}

// refreshIfExpired renews a Microsoft session before a launch uses it, and
// says plainly when only a fresh sign-in will do.
func refreshIfExpired(cmd *cobra.Command, manager *instance.Manager,
	store *launcher.AccountStore, account auth.Account) (auth.Account, error) {

	if account.Kind != auth.KindMSA || account.Usable() {
		return account, nil
	}

	client := auth.NewMSA(msaClientID(manager))
	if !client.Configured() {
		return account, auth.ErrNotConfigured
	}

	fmt.Fprintln(os.Stderr, "  session     renewing the Microsoft session")
	fresh, err := client.RefreshAccount(cmd.Context(), account)
	if err != nil {
		if errors.Is(err, auth.ErrReauth) {
			_ = store.MarkNeedsReauth(account.UUID)
			return account, fmt.Errorf("%s has to sign in again: run 'account login'", account.Name)
		}
		return account, err
	}
	if err := store.Add(fresh); err != nil {
		return account, err
	}
	return fresh, nil
}
