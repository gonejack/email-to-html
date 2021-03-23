package main

import (
	"log"
	"os"

	"github.com/gonejack/email-to-html/cmd"
	"github.com/spf13/cobra"
)

var (
	download = false
	verbose  = false

	prog = &cobra.Command{
		Use:   "email-to-html [-d] [-v] *.eml",
		Short: "Command line tool for converting emails to html.",
		Run: func(c *cobra.Command, args []string) {
			err := run(c, args)
			if err != nil {
				log.Fatal(err)
			}
		},
	}
)

func init() {
	log.SetOutput(os.Stdout)

	prog.Flags().SortFlags = false
	prog.PersistentFlags().SortFlags = false

	prog.PersistentFlags().BoolVarP(
		&download,
		"download",
		"d",
		false,
		"download remote images",
	)
	prog.PersistentFlags().BoolVarP(
		&verbose,
		"verbose",
		"v",
		false,
		"verbose",
	)
}

func run(c *cobra.Command, args []string) error {
	exec := cmd.EmailToHTML{
		ImagesDir:      "images",
		AttachmentsDir: "attachments",

		Download: download,
		Verbose:  verbose,
	}

	return exec.Execute(args)
}

func main() {
	_ = prog.Execute()
}
