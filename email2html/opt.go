package email2html

import (
	"path/filepath"

	"github.com/alecthomas/kong"
)

type Options struct {
	MediaDir       string   `default:"media" help:"Storage dir of images."`
	AttachmentDir  string   `default:"attachments" help:"Storage dir of attachments."`
	DownloadRemote bool     `short:"d"  help:"Download remote images."`
	Verbose        bool     `short:"v" help:"Verbose printing."`
	About          bool     `help:"About."`
	EML            []string `arg:"" optional:"" help:"list of .eml files"`
}

func MustParseOption() (opt Options) {
	kong.Parse(&opt,
		kong.Name("email-to-html"),
		kong.Description("This command line converts .eml file to .html file"),
		kong.UsageOnError(),
	)
	if len(opt.EML) == 0 || opt.EML[0] == "*.eml" {
		opt.EML, _ = filepath.Glob("*.eml")
	}
	return
}
