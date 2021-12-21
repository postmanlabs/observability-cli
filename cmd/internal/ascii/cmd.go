package ascii

import (
	"fmt"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:          "aki",
	Short:        "Show an ASCII Akita",
	Long:         "Show an ASCII Akita",
	SilenceUsage: true,
	Hidden:       true,
	RunE:         showAki,
}

var aki string = " ___                 ___\n >_ \\   _ _ _ _ _   / _<\n< <, Y``         ''Y .> >\n \\ (`  .-.-._.-.-.  ') /\n ( ` .'  _     _  `. ' )\n )  (   (')___(')   )  ( \n(  ('      `Y'      `)  )\n(   (.  `.__|__.'  .)   )\n (.  (._         _.)  .)\n  (_   `- - - - -'   _)\n    `-._ _ ___ _ _.-´  \n"

func showAki(cmd *cobra.Command, args []string) error {
	// TODO: colored version?
	fmt.Println(aki)
	return nil
}
