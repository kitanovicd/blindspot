package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/kitanovicd/blindspot/internal/terminal"
)

func (a *app) ask(ctx context.Context, label, fallback string) (string, error) {
	fmt.Fprintf(a.out, "\n  %s", a.outStyle.Text(terminal.Heading, label))
	if fallback != "" {
		fmt.Fprintf(a.out, " [%s]", fallback)
	}
	fmt.Fprint(a.out, "\n  > ")
	type answer struct {
		line string
		err  error
	}
	answers := make(chan answer, 1)
	go func() { line, err := a.input.ReadString('\n'); answers <- answer{line, err} }()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-answers:
		if result.err != nil {
			if result.err == io.EOF {
				return "", terminal.ErrCancelled
			}
			return "", result.err
		}
		line := strings.TrimSpace(result.line)
		if line == "" {
			line = fallback
		}
		return line, nil
	}
}

func (a *app) confirm(ctx context.Context, label string, fallback bool) (bool, error) {
	defaultAnswer := "no"
	if fallback {
		defaultAnswer = "yes"
	}
	for {
		value, err := a.ask(ctx, label+" (yes/no)", defaultAnswer)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(value) {
		case "yes", "y":
			return true, nil
		case "no", "n":
			return false, nil
		}
		fmt.Fprintln(a.out, "  Enter yes or no.")
	}
}

func (a *app) choose(ctx context.Context, title string, choices []terminal.Choice, multi bool) ([]int, error) {
	output, ok := a.out.(*os.File)
	if terminal.IsTerminal(os.Stdin) && ok && terminal.IsTerminal(output) && os.Getenv("TERM") != "dumb" && a.input.Buffered() == 0 {
		return terminal.Select(ctx, os.Stdin, a.out, title, choices, multi)
	}
	fmt.Fprintln(a.out, "\n  "+title)
	fmt.Fprintln(a.out)
	for i, choice := range choices {
		fmt.Fprintf(a.out, "  %d. %s\n", i+1, choice.Title)
		if choice.Detail != "" {
			fmt.Fprintf(a.out, "     %s\n", choice.Detail)
		}
	}
	label := "Choose a number (q to cancel)"
	if multi {
		label = "Choose numbers separated by commas, or all (q to cancel)"
	}
	for {
		value, err := a.ask(ctx, label, "")
		if err != nil {
			return nil, err
		}
		if value == "q" {
			return nil, terminal.ErrCancelled
		}
		if value == "all" && multi {
			result := make([]int, len(choices))
			for i := range result {
				result[i] = i
			}
			return result, nil
		}
		var result []int
		seen := map[int]bool{}
		valid := true
		for _, part := range strings.Split(value, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil || n < 1 || n > len(choices) {
				valid = false
				break
			}
			if !seen[n-1] {
				result = append(result, n-1)
				seen[n-1] = true
			}
		}
		if valid && (multi || len(result) == 1) {
			return result, nil
		}
		fmt.Fprintln(a.out, "  Enter a number from the list.")
	}
}

func (a *app) promptError(err error) int {
	if errors.Is(err, context.Canceled) || errors.Is(err, terminal.ErrCancelled) {
		return 130
	}
	return a.fail(err)
}

func (a *app) menu(ctx context.Context) int {
	a.banner(a.out)
	items := []terminal.Choice{
		{Title: "Run mutation tests"},
		{Title: "Free up space"},
		{Title: "Open a saved report"},
	}
	choice, err := a.choose(ctx, "Choose an action", items, false)
	if err != nil {
		return a.promptError(err)
	}
	switch choice[0] {
	case 0:
		return a.run(ctx, []string{"--interactive"})
	case 1:
		return a.clean(ctx, nil)
	default:
		return a.reports(ctx, nil)
	}
}
