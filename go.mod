module github.com/gonejack/email-to-html

go 1.16

require (
	github.com/PuerkitoBio/goquery v1.6.1
	github.com/bmaupin/go-epub v0.5.3
	github.com/dustin/go-humanize v1.0.0
	github.com/gabriel-vasile/mimetype v1.1.2
	github.com/jordan-wright/email v4.0.1-0.20210109023952-943e75fe5223+incompatible
	github.com/schollz/progressbar/v3 v3.7.4
	github.com/spf13/cobra v1.1.3
	golang.org/x/sync v0.0.0-20190423024810-112230192c58
)

replace github.com/jordan-wright/email => github.com/gonejack/email v1.0.1
