package cli

import (
	"errors"
	"flag"
	"os"
	"strings"
)

func defaultProfile() string {
	if v := os.Getenv("FOUNDRY_PROFILE"); v != "" {
		return v
	}
	return "default"
}

func (a *app) flags(name string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(a.errOut)
	return f
}

// parseFlags permits options before or after targets, while respecting --.
// Boolean options are discovered from the FlagSet so each flag is defined once.
func parseFlags(f *flag.FlagSet, args []string) error {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		name, _, inline := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		option := f.Lookup(name)
		if inline || option == nil {
			continue
		}
		boolean, ok := option.Value.(interface{ IsBoolFlag() bool })
		if ok && boolean.IsBoolFlag() {
			continue
		}
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		} else {
			// Let flag report the missing value before the synthetic separator
			// could be mistaken for the value of a string option.
			return f.Parse(flags)
		}
	}
	return f.Parse(append(append(flags, "--"), positional...))
}

func flagExit(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return 1
}
