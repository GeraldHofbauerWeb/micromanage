package launcher

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/auth"
)

// accountsFileName holds the signed-in accounts.
const accountsFileName = "accounts.json"

// accountsSchemaVersion is the format this build writes.
const accountsSchemaVersion = 1

// accountsFile is the on-disk shape.
type accountsFile struct {
	SchemaVersion int            `json:"schema_version"`
	Active        string         `json:"active,omitempty"`
	Accounts      []auth.Account `json:"accounts"`
}

// AccountStore persists accounts.
//
// The file is written 0600 because a Microsoft refresh token is in it. It is
// not encrypted: Go has no cross-platform keyring without cgo, and pretending
// otherwise would be worse than saying so plainly.
type AccountStore struct {
	path string

	mu       sync.RWMutex
	accounts []auth.Account
	active   string
}

// NewAccountStore loads the accounts stored under an application directory.
func NewAccountStore(appDir string) (*AccountStore, error) {
	s := &AccountStore{path: filepath.Join(appDir, accountsFileName)}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *AccountStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading %s: %w", accountsFileName, err)
	}

	var file accountsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parsing %s: %w", accountsFileName, err)
	}
	if file.SchemaVersion > accountsSchemaVersion {
		return fmt.Errorf("%s was written by a newer version (schema %d)", accountsFileName, file.SchemaVersion)
	}

	s.accounts, s.active = file.Accounts, file.Active
	return nil
}

// save writes the accounts atomically with restrictive permissions.
func (s *AccountStore) save() error {
	file := accountsFile{
		SchemaVersion: accountsSchemaVersion,
		Active:        s.active,
		Accounts:      s.accounts,
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", accountsFileName, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return err
	}
	// Rename preserves the temporary file's mode, but be explicit in case the
	// destination already existed with looser permissions.
	return os.Chmod(s.path, 0o600)
}

// List returns the accounts and the active one's UUID.
func (s *AccountStore) List() ([]auth.Account, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]auth.Account(nil), s.accounts...), s.active
}

// Active returns the account a launch should use.
func (s *AccountStore) Active() (auth.Account, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, a := range s.accounts {
		if a.UUID == s.active {
			return a, true
		}
	}
	// A single account is unambiguous even without an explicit choice.
	if len(s.accounts) == 1 {
		return s.accounts[0], true
	}
	return auth.Account{}, false
}

// Add stores an account and makes it active, replacing one with the same UUID.
func (s *AccountStore) Add(account auth.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	account.LastUsed = time.Now().UTC()

	replaced := false
	for i, existing := range s.accounts {
		if existing.UUID == account.UUID {
			s.accounts[i] = account
			replaced = true
			break
		}
	}
	if !replaced {
		s.accounts = append(s.accounts, account)
	}
	s.active = account.UUID

	return s.save()
}

// SetActive chooses which account launches.
func (s *AccountStore) SetActive(uuid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, a := range s.accounts {
		if a.UUID == uuid {
			s.active = uuid
			return s.save()
		}
	}
	return fmt.Errorf("no account with id %q", uuid)
}

// Remove deletes an account.
func (s *AccountStore) Remove(uuid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	kept := s.accounts[:0]
	for _, a := range s.accounts {
		if a.UUID != uuid {
			kept = append(kept, a)
		}
	}
	s.accounts = kept

	if s.active == uuid {
		s.active = ""
		if len(s.accounts) > 0 {
			s.active = s.accounts[0].UUID
		}
	}
	return s.save()
}
