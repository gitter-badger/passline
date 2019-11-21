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

func (pl *Passline) DisplayItem(c *ucli.Context) error {
	names, err := pl.store.GetAllNames()
	handle(err)

	if len(names) == 0 {
		renderer.NoItemsExist()
		os.Exit(0)
	}

	args := c.Args()
	renderer.DisplayMessage()

	name, err := cli.ArgOrSelect(args, 0, "URL", names)
	handle(err)

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

		username, err := cli.ArgOrSelect(args, 1, "Username/Logic", item.GetUsernameArray())
		handle(err)

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

func (pl *Passline) GenerateItem(c *ucli.Context) error {
	args := c.Args()
	renderer.CreateMessage()

	// User input name
	name, err := cli.ArgOrInput(args, 0, "URL", "")
	handle(err)

	// User input username
	username, err := cli.ArgOrInput(args, 1, "Username/Login", "")
	handle(err)

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
	names, err := pl.store.GetAllNames()
	handle(err)

	if len(names) == 0 {
		renderer.NoItemsExist()
		os.Exit(0)
	}

	args := c.Args()
	renderer.DeleteMessage()

	name, err := cli.ArgOrSelect(args, 0, "URL", names)
	handle(err)

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
		username, err := cli.ArgOrInput(args, 1, "Please enter the Username/Login []: ", "")
		handle(err)

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
	names, err := pl.store.GetAllNames()
	handle(err)

	if len(names) == 0 {
		renderer.NoItemsExist()
		os.Exit(0)
	}

	args := c.Args()
	renderer.ChangeMessage()

	name, err := cli.ArgOrSelect(args, 0, "URL", names)
	handle(err)

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
		username, err := cli.ArgOrSelect(args, 1, "Username/Login", item.GetUsernameArray())
		handle(err)

		// Check if name, username combination exists
		_, err = item.GetCredentialByUsername(username)
		if err != nil {
			renderer.InvalidUsername(name, username)
			os.Exit(0)
		}
	}

	// Get new username
	newUsername, err := cli.Input("Please enter a new Username/Login []: (%s) ", username)
	handle(err)

	if newUsername == "" {
		newUsername = username
	}

	for i := 0; i < len(item.Credentials); i++ {
		if item.Credentials[i].Username == username {
			item.Credentials[i].Username = newUsername
		}
	}

	err = pl.store.UpdateItem(item)
	handle(err)

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

func handle(err error) {
	if err != nil {
		os.Exit(1)
	}
}
