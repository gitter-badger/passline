package core

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/atotto/clipboard"
	ucli "github.com/urfave/cli"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/perryrh0dan/passline/pkg/cli"
	"github.com/perryrh0dan/passline/pkg/config"
	"github.com/perryrh0dan/passline/pkg/crypt"
	"github.com/perryrh0dan/passline/pkg/renderer"
	"github.com/perryrh0dan/passline/pkg/storage"
)

type Passline struct {
	config *config.Config
	store  storage.Storage
}

func NewPassline() *Passline {
	pl := new(Passline)
	pl.config, _ = config.Get()
	switch pl.config.Storage {
	case "firestore":
		pl.store = &storage.FireStore{}
	default:
		pl.store = &storage.LocalStorage{}
	}
	err := pl.store.Init()
	if err != nil {
		renderer.StorageError()
		os.Exit(1)
	}
	return pl
}

func (pl *Passline) getPassword(c *ucli.Context) ([]byte, error) {
	password := []byte(c.String("password"))

	if len(password) <= 0 {
		// Get global password
		fmt.Print("Enter Global Password: ")

		// Ask for global password
		var err error
		password, err = terminal.ReadPassword(int(syscall.Stdin))
		if err != nil {
			return nil, err
		}

		fmt.Println()
	}

	valid, err := pl.checkPassword(password)
	if err != nil || !valid {
		return nil, errors.New("Invalid password")
	}

	return password, nil
}

func (pl *Passline) checkPassword(password []byte) (bool, error) {
	data, err := pl.store.GetAll()
	if err != nil {
		return false, err
	}

	if len(data) == 0 {
		return true, nil
	}

	item, err := pl.store.GetByIndex(0)
	if err != nil {
		return false, err
	}

	_, err = crypt.AesGcmDecrypt(password, item.Credentials[0].Password)
	if err != nil {
		renderer.InvalidPassword()
		return false, err
	}

	return true, nil
}

func (pl *Passline) getValue(args []string, index int, message string, values []string, items []string) (string, error) {
	// Check if Selection is active
	if pl.config.Selection {
		// Build message string
		message = fmt.Sprintf("Please select a %s", message)
		name, err := cli.ArgOrSelect(args, index, message, items)
		if err != nil {
			return "", err
		}
		return name, nil
	} else {
		// Build message string
		message = fmt.Sprintf("Please enter a %s", message)
		name, err := cli.ArgOrInput(args, index, message, values)
		if err != nil {
			return "", err
		}
		return name, nil
	}
}

// DisplayByName the password
func (pl *Passline) DisplayByName(c *ucli.Context) error {
	args := c.Args()
	renderer.DisplayMessage()

	var names []string
	items, _ := pl.store.GetAll()
	for _, item := range items {
		names = append(names, item.Name)
	}
	name, err := pl.getValue(args, 0, "URL:", nil, names)
	if err != nil {
		os.Exit(1)
	}

	// Check if item for name exists
	item, err := pl.store.GetByName(name)
	if err != nil {
		renderer.InvalidName(name)
		return nil
	}

	var credential storage.Credential
	// Only need username for multiple credentials
	if len(item.Credentials) > 1 {
		// User input username

		username, err := pl.getValue(args, 1, "Username/Logic", nil, item.GetUsernameArray())
		if err != nil {
			os.Exit(1)
		}

		// Check if name, username combination exists
		item, err := pl.store.GetByName(name)
		if err == nil {
			credential, err = item.GetCredentialByUsername(username)
			if err != nil {
				renderer.InvalidUsername(name, username)
				os.Exit(0)
			}
		}
	} else {
		credential = item.Credentials[0]
	}

	// Get and Check for global password.
	globalPassword, err := pl.getPassword(c)
	if err != nil {
		return nil
	}

	// Decrypt passwords
	credential.Password, err = crypt.AesGcmDecrypt(globalPassword, credential.Password)
	if err != nil {
		os.Exit(0)
	}

	// Display item and copy password to clipboard
	renderer.DisplayCredential(credential)
	err = clipboard.WriteAll(credential.Password)
	if err != nil {
		renderer.ClipboardError()
		return nil
	}

	renderer.SuccessfulCopiedToClipboard(item.Name, credential.Username)
	return nil
}

// Generate a random password for a item
func (pl *Passline) GenerateForSite(c *ucli.Context) error {
	args := c.Args()
	renderer.CreateMessage()

	// User input name
	name, err := cli.ArgOrInput(args, 0, "Please enter the URL []: ", nil)
	if err != nil {
		os.Exit(1)
	}

	// User input username
	username, err := cli.ArgOrInput(args, 1, "Please enter the Username/Login []: ", nil)
	if err != nil {
		os.Exit(1)
	}

	// Check if name, username combination exists
	item, err := pl.store.GetByName(name)
	if err == nil {
		_, err = item.GetCredentialByUsername(username)
		if err == nil {
			return nil
		}
	}

	// Get and Check for global password.
	globalPassword, err := pl.getPassword(c)
	if err != nil {
		return nil
	}

	// Generate password and crypt password
	password := generatePassword(20)
	cryptedPassword, err := crypt.AesGcmEncrypt(globalPassword, password)

	// Create Credentials
	credential := storage.Credential{Username: username, Password: cryptedPassword}

	// Check if item already exists
	item, err = pl.store.GetByName(name)
	if err != nil {
		// Generate new item entry
		item := storage.Item{Name: name, Credentials: []storage.Credential{credential}}
		err = pl.store.AddItem(item)
		if err != nil {
			os.Exit(0)
		}
	} else {
		// Add to existing item
		err := pl.store.AddCredential(name, credential)
		if err != nil {
			os.Exit(0)
		}
	}

	err = clipboard.WriteAll(password)
	if err != nil {
		renderer.ClipboardError()
		os.Exit(0)
	}

	renderer.SuccessfulCopiedToClipboard(name, username)
	return nil
}

func (pl *Passline) DeleteItem(c *ucli.Context) error {
	args := c.Args()

	name, err := cli.ArgOrInput(args, 0, "Please enter the URL []: ", nil)
	if err != nil {
		os.Exit(1)
	}

	item, err := pl.store.GetByName(name)
	if err != nil {
		renderer.InvalidName(name)
		os.Exit(0)
	}

	if len(item.Credentials) <= 1 {
		// If Item has only one Credential delete item
		err = pl.store.DeleteItem(item)
		if err != nil {
			os.Exit(0)
		}
	} else {
		// If Item has multiple Credentials ask for username
		// User input username
		username, err := cli.ArgOrInput(args, 1, "Please enter the Username/Login []: ", nil)
		if err != nil {
			os.Exit(1)
		}

		// Check if name, username combination exists
		var credential storage.Credential
		credential, err = item.GetCredentialByUsername(username)
		if err != nil {
			renderer.InvalidUsername(name, username)
			os.Exit(0)
		}

		err = pl.store.DeleteCredential(item, credential)
		if err != nil {
			os.Exit(0)
		}
	}

	return nil
}

func (pl *Passline) EditItem(c *ucli.Context) error {
	args := c.Args()
	renderer.ChangeMessage()

	// User input name
	name, err := cli.ArgOrInput(args, 0, "Please enter the URL []: ", nil)
	if err != nil {
		os.Exit(1)
	}

	item, err := pl.store.GetByName(name)
	if err != nil {
		renderer.InvalidName(name)
		os.Exit(0)
	}

	username := ""
	if len(item.Credentials) == 1 {
		username = item.Credentials[0].Username
	} else {
		// User input username
		username, err := cli.ArgOrInput(args, 1, "Please enter the Username/Login []: ", nil)
		if err != nil {
			os.Exit(1)
		}

		// Check if name, username combination exists
		_, err = item.GetCredentialByUsername(username)
		if err != nil {
			renderer.InvalidUsername(name, username)
			os.Exit(0)
		}
	}

	// Get new username
	newUsername := cli.Input("Please enter a new Username/Login []: (%s) ", []string{username})
	if newUsername == "" {
		newUsername = username
	}

	// Get new password
	// newPassword := getInput("Please enter a new Password []: (********)")

	for i := 0; i < len(item.Credentials); i++ {
		if item.Credentials[i].Username == username {
			item.Credentials[i].Username = newUsername
		}
	}

	err = pl.store.UpdateItem(item)
	if err != nil {
		os.Exit(1)
	}

	renderer.SuccessfulChangedItem()

	return nil
}

func (pl *Passline) ListSites(c *ucli.Context) error {
	args := c.Args()

	if len(args) >= 1 {
		item, err := pl.store.GetByName(args[0])
		if err != nil {
			renderer.InvalidName(args[0])
			os.Exit(0)
		}

		renderer.DisplayItem(item)
	} else {
		items, err := pl.store.GetAll()
		if err != nil {
			return nil
		}

		if len(items) == 0 {
			renderer.NoItemsMessage()
		}
		renderer.DisplayItems(items)
	}

	return nil
}

func generatePassword(length int) string {
	rand.Seed(time.Now().UnixNano())
	chars := []rune("ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
		"abcdefghijklmnopqrstuvwxyz" +
		"0123456789" +
		"!$%&()/?")
	var b strings.Builder
	for i := 0; i < length; i++ {
		b.WriteRune(chars[rand.Intn(len(chars))])
	}
	password := b.String() // E.g. "ExcbsVQs"
	return password
}
