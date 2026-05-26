package config

import (
	"encoding/json"
	"fmt"
	"os"
	"post-gen/internal/models"
	"time"
)

// LoadAccounts loads the account configurations from accounts.json.
func LoadAccounts(path string) ([]models.Account, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var accounts []models.Account
	err = json.Unmarshal(data, &accounts)
	if err != nil {
		return nil, err
	}

	return accounts, nil
}

// SaveAccounts saves the account configurations to accounts.json.
// It creates a backup of the existing file before overwriting.
func SaveAccounts(path string, accounts []models.Account) error {
	// Backup existing file if it exists
	if _, err := os.Stat(path); err == nil {
		backupPath := fmt.Sprintf("%s.bak-%s", path, time.Now().Format("20060102150405"))
		data, err := os.ReadFile(path)
		if err == nil {
			_ = os.WriteFile(backupPath, data, 0644)
		}
	}

	data, err := json.MarshalIndent(accounts, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal accounts: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to save accounts: %w", err)
	}

	return nil
}
