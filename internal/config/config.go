package config

import (
	"encoding/json"
	"os"
	"post-gen/internal/models"
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
