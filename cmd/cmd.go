package cmd

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io/ioutil"
	"log"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gabriel-vasile/mimetype"
	"github.com/gonejack/email"
	"github.com/gonejack/get"
)

type EmailToHTML struct {
	ImagesDir      string
	AttachmentsDir string

	Download bool
	Verbose  bool
}

func (c *EmailToHTML) Execute(emails []string) error {
	if len(emails) == 0 {
		emails, _ = filepath.Glob("*.eml")
	}
	if len(emails) == 0 {
		return errors.New("no eml given")
	}

	err := c.mkdir()
	if err != nil {
		return err
	}

	for _, eml := range emails {
		log.Printf("convert %s", eml)

		mail, err := c.openEmail(eml)
		if err != nil {
			return err
		}

		attachments, err := c.extractAttachment(eml, mail)
		if err != nil {
			return fmt.Errorf("cannot extract attachments %s", err)
		}

		document, err := goquery.NewDocumentFromReader(bytes.NewReader(mail.HTML))
		if err != nil {
			return fmt.Errorf("cannot parse HTML: %s", err)
		}
		document = c.cleanDoc(document)

		var saveImages map[string]string
		if c.Download {
			saveImages = c.saveImages(document)
		}

		document.Find("img").Each(func(i int, img *goquery.Selection) {
			c.changeRef(img, attachments, saveImages)
		})

		title := c.renderTitle(mail)
		if document.Find("title").Length() == 0 {
			document.Find("head").AppendHtml(fmt.Sprintf("<title>%s</title>", html.EscapeString(title)))
		}
		if document.Find("title").Text() == "" {
			document.Find("title").SetText(title)
		}

		_, exist := document.Find("html").Attr("lang")
		if !exist && mail.Headers.Get("Content-Language") != "" {
			document.Find("html").SetAttr("lang", mail.Headers.Get("Content-Language"))
		}

		content, err := document.Html()
		if err != nil {
			return fmt.Errorf("cannot generate body: %s", err)
		}

		filename := strings.TrimSuffix(eml, filepath.Ext(eml)) + ".html"
		err = ioutil.WriteFile(filename, []byte(content), 0766)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *EmailToHTML) mkdir() error {
	err := os.MkdirAll(c.ImagesDir, 0777)
	if err != nil {
		return fmt.Errorf("cannot make images dir %s", err)
	}
	err = os.MkdirAll(c.AttachmentsDir, 0777)
	if err != nil {
		return fmt.Errorf("cannot make attachments dir %s", err)
	}

	return nil
}
func (c *EmailToHTML) openEmail(eml string) (*email.Email, error) {
	file, err := os.Open(eml)
	if err != nil {
		return nil, fmt.Errorf("cannot open file: %s", err)
	}
	defer file.Close()
	mail, err := email.NewEmailFromReader(file)
	if err != nil {
		return nil, fmt.Errorf("cannot parse email: %s", err)
	}
	return mail, nil
}

func (c *EmailToHTML) saveImages(doc *goquery.Document) map[string]string {
	downloads := make(map[string]string)

	var refs, paths []string
	doc.Find("img").Each(func(i int, img *goquery.Selection) {
		src, _ := img.Attr("src")
		if !strings.HasPrefix(src, "http") {
			return
		}

		localFile, exist := downloads[src]
		if exist {
			return
		}

		uri, err := url.Parse(src)
		if err != nil {
			log.Printf("parse %s fail: %s", src, err)
			return
		}
		localFile = filepath.Join(c.ImagesDir, fmt.Sprintf("%s%s", md5str(src), filepath.Ext(uri.Path)))

		refs = append(refs, src)
		paths = append(paths, localFile)
		downloads[src] = localFile
	})

	getter := get.DefaultGetter()
	getter.Verbose = c.Verbose
	eRefs, errs := getter.BatchInOrder(refs, paths, 3, time.Minute*2)
	for i := range eRefs {
		log.Printf("download %s fail: %s", eRefs[i], errs[i])
	}

	return downloads
}
func (c *EmailToHTML) extractAttachment(eml string, mail *email.Email) (attachments map[string]string, err error) {
	attachments = make(map[string]string)
	for i, a := range mail.Attachments {
		if c.Verbose {
			log.Printf("extract %s", a.Filename)
		}

		saveFile := filepath.Join(c.AttachmentsDir, fmt.Sprintf("%s.a%d.%s", md5str(eml), i, a.Filename))
		err = ioutil.WriteFile(saveFile, a.Content, 0777)
		if err != nil {
			log.Printf("cannot extact image %s", a.Filename)
			continue
		}
		cid := a.Header.Get("Content-ID")
		cid = strings.TrimPrefix(cid, "<")
		cid = strings.TrimSuffix(cid, ">")
		attachments[cid] = saveFile
		attachments[a.Filename] = saveFile
	}
	return
}
func (c *EmailToHTML) changeRef(img *goquery.Selection, attachments, downloads map[string]string) {
	img.RemoveAttr("loading")
	img.RemoveAttr("srcset")

	src, _ := img.Attr("src")

	switch {
	case strings.HasPrefix(src, "http"):
		localFile, exist := downloads[src]
		if !exist {
			return
		}

		if c.Verbose {
			log.Printf("replace %s as %s", src, localFile)
		}

		// check mime
		fmime, err := mimetype.DetectFile(localFile)
		if err != nil {
			log.Printf("cannot detect image mime of %s: %s", src, err)
			return
		}
		if !strings.HasPrefix(fmime.String(), "image") {
			img.Remove()
			log.Printf("mime of %s is %s instead of images", src, fmime.String())
			return
		}

		img.SetAttr("src", localFile)
	case strings.HasPrefix(src, "cid:"):
		contentId := strings.TrimPrefix(src, "cid:")

		localFile, exist := attachments[contentId]
		if !exist {
			log.Printf("content id %s not found", contentId)
			return
		}

		if c.Verbose {
			log.Printf("replace %s as %s", src, localFile)
		}

		img.SetAttr("src", localFile)
	default:
		log.Printf("unsupported image reference[src=%s]", src)
	}
}

func (_ *EmailToHTML) renderTitle(mail *email.Email) string {
	title := mail.Subject
	decoded, err := decodeRFC2047(title)
	if err == nil {
		title = decoded
	}
	return title
}
func (_ *EmailToHTML) cleanDoc(doc *goquery.Document) *goquery.Document {
	// remove inoreader ads
	doc.Find("body").Find(`div:contains("ads from inoreader")`).Closest("center").Remove()

	return doc
}

func decodeRFC2047(word string) (string, error) {
	isRFC2047 := strings.HasPrefix(word, "=?") && strings.Contains(word, "?=")
	if isRFC2047 {
		isRFC2047 = strings.Contains(word, "?Q?") || strings.Contains(word, "?B?")
	}
	if !isRFC2047 {
		return word, nil
	}

	comps := strings.Split(word, "?")
	if len(comps) < 5 {
		return word, nil
	}

	if comps[2] == "B" && strings.HasSuffix(comps[3], "=") {
		b64s := strings.TrimRight(comps[3], "=")
		text, _ := base64.RawURLEncoding.DecodeString(b64s)
		comps[3] = base64.StdEncoding.EncodeToString(text)
	}

	return new(mime.WordDecoder).DecodeHeader(strings.Join(comps, "?"))
}
func md5str(s string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(s)))
}
