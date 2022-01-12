package main

import (
	"log"

	"github.com/gonejack/email-to-html/cmd"
)

func main() {
	var c cmd.EmailToHTML

	if e := c.Run(); e != nil {
		log.Fatal(e)
	}
}
