package persistence

import (
	"encoding/base64"
	"fmt"

	uuid "github.com/gofrs/uuid"
	"github.com/offen/offen/server/keys"
)

type BootstrapConfig struct {
	Accounts     []BootstrapAccount     `yaml:"accounts"`
	AccountUsers []BootstrapAccountUser `yaml:"account_users"`
}

type BootstrapAccount struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
}

type BootstrapAccountUser struct {
	Email    string   `yaml:"email"`
	Password string   `yaml:"password"`
	Accounts []string `yaml:"accounts"`
}

type accountCreation struct {
	encryptionKey []byte
	account       Account
}

// Bootstrap seeds a blank database with the given account and user
// data. This is likely only ever used in development.
func (r *relationalDatabase) Bootstrap(config BootstrapConfig, emailSalt []byte) error {
	txn, err := r.db.Transaction()
	if err != nil {
		return fmt.Errorf("persistence: error creating transaction: %w", err)
	}
	if err := txn.DropAll(); err != nil {
		txn.Rollback()
		return fmt.Errorf("persistence: error dropping tables before inserting seed data: %w", err)
	}

	accounts, accountUsers, relationships, err := bootstrapAccounts(&config, emailSalt)
	if err != nil {
		txn.Rollback()
		return fmt.Errorf("persistence: error creating seed data: %w", err)
	}
	for _, account := range accounts {
		if err := txn.CreateAccount(&account); err != nil {
			txn.Rollback()
			return fmt.Errorf("persistence: error creating account: %w", err)
		}
	}
	for _, accountUser := range accountUsers {
		if err := txn.CreateAccountUser(&accountUser); err != nil {
			txn.Rollback()
			return fmt.Errorf("persistence: error creating account user: %w", err)
		}
	}
	for _, relationship := range relationships {
		if err := txn.CreateAccountUserRelationship(&relationship); err != nil {
			txn.Rollback()
			return fmt.Errorf("persistence: error creating account user relationship: %w", err)
		}
	}
	if err := txn.Commit(); err != nil {
		return fmt.Errorf("persistence: error committing seed data: %w", err)
	}
	return nil
}

func bootstrapAccounts(config *BootstrapConfig, emailSalt []byte) ([]Account, []AccountUser, []AccountUserRelationship, error) {
	accountCreations := []accountCreation{}
	for _, account := range config.Accounts {
		publicKey, privateKey, keyErr := keys.GenerateRSAKeypair(keys.RSAKeyLength)
		if keyErr != nil {
			return nil, nil, nil, keyErr
		}

		encryptionKey, encryptionKeyErr := keys.GenerateEncryptionKey(keys.DefaultEncryptionKeySize)
		if encryptionKeyErr != nil {
			return nil, nil, nil, encryptionKeyErr
		}
		encryptedPrivateKey, privateKeyNonce, encryptedPrivateKeyErr := keys.EncryptWith(encryptionKey, privateKey)
		if encryptedPrivateKeyErr != nil {
			return nil, nil, nil, encryptedPrivateKeyErr
		}

		salt, saltErr := keys.GenerateRandomValue(keys.DefaultSecretLength)
		if saltErr != nil {
			return nil, nil, nil, saltErr
		}

		record := Account{
			AccountID: account.ID,
			Name:      account.Name,
			PublicKey: string(publicKey),
			EncryptedPrivateKey: fmt.Sprintf(
				"%s %s",
				base64.StdEncoding.EncodeToString(privateKeyNonce),
				base64.StdEncoding.EncodeToString(encryptedPrivateKey),
			),
			UserSalt: salt,
			Retired:  false,
		}
		accountCreations = append(accountCreations, accountCreation{
			account:       record,
			encryptionKey: encryptionKey,
		})
	}

	accountUserCreations := []AccountUser{}
	relationshipCreations := []AccountUserRelationship{}

	for _, accountUser := range config.AccountUsers {
		userID, idErr := uuid.NewV4()
		if idErr != nil {
			return nil, nil, nil, idErr
		}
		hashedPw, hashedPwErr := keys.HashPassword(accountUser.Password)
		if hashedPwErr != nil {
			return nil, nil, nil, hashedPwErr
		}
		hashedEmail, hashedEmailErr := keys.HashEmail(accountUser.Email, emailSalt)
		if hashedEmailErr != nil {
			return nil, nil, nil, hashedEmailErr
		}
		salt, saltErr := keys.GenerateRandomValue(8)
		if saltErr != nil {
			return nil, nil, nil, saltErr
		}
		saltBytes, _ := base64.StdEncoding.DecodeString(salt)
		user := AccountUser{
			UserID:         userID.String(),
			Salt:           salt,
			HashedPassword: base64.StdEncoding.EncodeToString(hashedPw),
			HashedEmail:    base64.StdEncoding.EncodeToString(hashedEmail),
		}
		accountUserCreations = append(accountUserCreations, user)

		for _, accountID := range accountUser.Accounts {
			var encryptionKey []byte
			for _, creation := range accountCreations {
				if creation.account.AccountID == accountID {
					encryptionKey = creation.encryptionKey
					break
				}
			}
			if encryptionKey == nil {
				return nil, nil, nil, fmt.Errorf("account with id %s not found", accountID)
			}

			passwordDerivedKey, passwordDerivedKeyErr := keys.DeriveKey(accountUser.Password, saltBytes)
			if passwordDerivedKeyErr != nil {
				return nil, nil, nil, passwordDerivedKeyErr
			}
			encryptedPasswordDerivedKey, passwordEncryptionNonce, encryptionErr := keys.EncryptWith(passwordDerivedKey, encryptionKey)
			if encryptionErr != nil {
				return nil, nil, nil, encryptionErr
			}

			emailDerivedKey, emailDerivedKeyErr := keys.DeriveKey(accountUser.Email, saltBytes)
			if emailDerivedKeyErr != nil {
				return nil, nil, nil, emailDerivedKeyErr
			}
			encryptedEmailDerivedKey, emailEncryptionNonce, encryptionErr := keys.EncryptWith(emailDerivedKey, encryptionKey)
			if encryptionErr != nil {
				return nil, nil, nil, encryptionErr
			}

			relationshipID, idErr := uuid.NewV4()
			if idErr != nil {
				return nil, nil, nil, idErr
			}
			r := AccountUserRelationship{
				RelationshipID: relationshipID.String(),
				UserID:         userID.String(),
				AccountID:      accountID,
				PasswordEncryptedKeyEncryptionKey: fmt.Sprintf(
					"%s %s",
					base64.StdEncoding.EncodeToString(passwordEncryptionNonce),
					base64.StdEncoding.EncodeToString(encryptedPasswordDerivedKey),
				),
				EmailEncryptedKeyEncryptionKey: fmt.Sprintf(
					"%s %s",
					base64.StdEncoding.EncodeToString(emailEncryptionNonce),
					base64.StdEncoding.EncodeToString(encryptedEmailDerivedKey),
				),
			}
			relationshipCreations = append(relationshipCreations, r)
		}
	}
	var accounts []Account
	for _, creation := range accountCreations {
		accounts = append(accounts, creation.account)
	}
	return accounts, accountUserCreations, relationshipCreations, nil
}