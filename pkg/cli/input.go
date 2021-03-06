package cli

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

func Input(message string, defaultValue string) (string, error) {
	// find if %s is in string
	rgx := regexp.MustCompile("%s")
	matches := rgx.FindAllStringIndex(message, -1)

	// print message
	if len(matches) == 0 {
		fmt.Print(message)
	} else {
		fmt.Printf(message, defaultValue)
	}

	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	text = strings.TrimSuffix(text, "\n")
	// TODO Test if working for Linux
	text = strings.TrimSuffix(text, "\r")

	// if input empty assign default value also valid if defaultValue is empty
	if text == "" {
		text = defaultValue
	}
	return text, nil
}
