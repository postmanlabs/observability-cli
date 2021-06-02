package man

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/gdamore/tcell/v2"
	"github.com/pkg/errors"
	"github.com/rivo/tview"
	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
)

var (
	// If true, dump raw, unformatted markdown.
	rawFlag bool
)

const (
	rootPageID = "xxx-akita-man-root-page"
)

type page struct {
	command string
	content string
}

// Put backticks in to workaround go's raw string literal limitation on now
// backticks.
func addBackticks(s string) string {
	return strings.ReplaceAll(s, "<bt>", "`")
}

var (
	// Using a list instead of map to ensure ordering.
	allPages = []page{
		{
			command: "apidiff",
			content: addBackticks(apidiffPage),
		},
		{
			command: "apidump",
			content: addBackticks(apidumpPage),
		},
		{
			command: "apispec",
			content: addBackticks(apispecPage),
		},
		{
			command: "learn",
			content: addBackticks(learnPage),
		},
		{
			command: "setversion",
			content: addBackticks(setversionPage),
		},
		{
			command: "upload",
			content: addBackticks(uploadPage),
		},
	}
)

func init() {
	Cmd.Flags().BoolVar(
		&rawFlag,
		"raw",
		false,
		"If true, output raw unformatted markdown.")
}

var Cmd = &cobra.Command{
	Use:          "man [COMMAND]",
	Short:        "Manual pages for Akita commands.",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		command := ""
		if len(args) > 0 {
			command = args[0]
		}

		// Verify that the positional argument refers to a known page.
		if rawFlag || command != "" {
			var targetPage *page
			for _, p := range allPages {
				if p.command == command {
					targetPage = &p
					break
				}
			}

			if targetPage == nil {
				return errors.Errorf("unknown command %q", command)
			} else if rawFlag {
				fmt.Println(targetPage.content)
				return nil
			}
		}

		if err := showInteractive(command); err != nil {
			return cmderr.AkitaErr{Err: err}
		}
		return nil
	},
}

func showInteractive(command string) error {
	app := tview.NewApplication()

	// Allow user to use 'q' to quit.
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if r := event.Rune(); r == 'q' || r == 'Q' {
			app.Stop()
		}
		return event
	})

	app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		// Clear the screen on every draw so each man page maintains a clear
		// background.
		screen.Clear()
		return false
	})

	// Create a page for each command.
	manPages := tview.NewPages()
	if err := createPages(manPages); err != nil {
		return errors.Wrap(err, "failed to create command pages")
	}

	// Create a directory list.
	list := tview.NewList()
	for i, p := range allPages {
		// Avoid capturing loop variable.
		loopCmd := p.command
		list.AddItem(loopCmd, "", 'a'+rune(i), func() {
			manPages.SwitchToPage(loopCmd)
		})
	}

	rootPage := manPages.AddPage(rootPageID, list, true, true)
	rootPage.SetBackgroundColor(tcell.ColorDefault)

	rootFrame := tview.NewFrame(rootPage)
	rootFrame.AddText(`q to quit, ESC to go back`, false, 0, tcell.ColorYellow)
	rootFrame.SetBackgroundColor(tcell.ColorDefault)

	// User has specified a command, switch that page by default.
	if command != "" {
		manPages.SwitchToPage(command)
	}

	return app.SetRoot(rootFrame, true).SetFocus(rootFrame).Run()
}

func createPages(pages *tview.Pages) error {
	for _, p := range allPages {
		txt := tview.NewTextView()
		txt.SetDynamicColors(true)
		txt.SetScrollable(true)
		txt.SetDoneFunc(func(k tcell.Key) {
			// Go back to root page when user presses ESC. The help text is included
			// in the rootFrame above.
			if k == tcell.KeyEsc {
				pages.SwitchToPage(rootPageID)
			}
		})
		txt.SetBackgroundColor(tcell.ColorDefault)

		// Fallback to print the unformatted content.
		printContent := p.content
		if md, err := formatMarkdown(printContent); err == nil {
			printContent = md
		}

		if _, err := io.Copy(tview.ANSIWriter(txt), strings.NewReader(printContent)); err != nil {
			return errors.Wrapf(err, "failed to write markdown for %s", p.command)
		}

		pages.AddPage(p.command, txt, true, false)
	}

	return nil
}

func formatMarkdown(content string) (string, error) {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(80),
	)
	if err != nil {
		return "", err
	}
	return renderer.Render(content)
}
