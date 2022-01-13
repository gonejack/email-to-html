package main

import (
	"log"

	"github.com/gonejack/email-to-html/email2html"
)

func main() {
	cmd := email2html.EmailToHTML{
		Options: email2html.MustParseOption(),
	}
	err := cmd.Run()
	if err != nil {
		log.Fatal(err)
	}
}
