package main

import (
	"errors"
	"os"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/config"
)

// The prefix applies only to human-readable CLI copy. Protocol values and
// JSON output never use it. Precedence: --lang, CLAWDLINE_LANG, saved
// product_language, English. A well-formed but unsupported tag renders English.
var commandLanguage string

func commandLanguagePrefix(args []string) ([]string, string, error) {
	if len(args) <= 1 || args[1] != "--lang" {
		return args, "", nil
	}
	if len(args) < 4 || !nextconfig.ValidProductLanguageTag(args[2]) {
		return args, "", errors.New(cliCopy("core", "language.action_missing", "--lang requires a tag and a command"))
	}
	return append([]string{args[0]}, args[3:]...), nextconfig.ResolveProductLanguage(args[2]), nil
}

func cliProductLanguage(flag, environment string, values nextconfig.Values) string {
	if flag != "" {
		return nextconfig.ResolveProductLanguage(flag)
	}
	if strings.TrimSpace(environment) != "" {
		return nextconfig.ResolveProductLanguage(environment)
	}
	return nextconfig.ProductLanguage(values)
}

func currentCLILanguage() string {
	values, err := nextconfig.Open(config.Dir()).Read()
	if err != nil {
		return cliProductLanguage(commandLanguage, os.Getenv("CLAWDLINE_LANG"), nextconfig.Values{})
	}
	return cliProductLanguage(commandLanguage, os.Getenv("CLAWDLINE_LANG"), values)
}
