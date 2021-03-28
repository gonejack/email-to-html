package cmd

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"io/ioutil"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/dustin/go-humanize"
	"github.com/gabriel-vasile/mimetype"
	"github.com/jordan-wright/email"
	"github.com/schollz/progressbar/v3"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

type EmailToHTML struct {
	client http.Client

	ImagesDir      string
	AttachmentsDir string

	Download bool
	Verbose  bool
}

func (c *EmailToHTML) Execute(emails []string) error {
	if len(emails) == 0 {
		return errors.New("no eml given")
	}

	err := c.mkdirs()
	if err != nil {
		return err
	}

	for _, eml := range emails {
		log.Printf("convert %s", eml)

		mail, err := c.openEmail(eml)
		if err != nil {
			return err
		}

		attachments, err := c.extractAttachments(mail)
		if err != nil {
			return fmt.Errorf("cannot extract attachments %s", err)
		}

		document, err := goquery.NewDocumentFromReader(bytes.NewReader(mail.HTML))
		if err != nil {
			return fmt.Errorf("cannot parse HTML: %s", err)
		}
		document = c.cleanDoc(document)

		var downloads map[string]string
		if c.Download {
			downloads = c.downloadImages(document)
		}

		document.Find("img").Each(func(i int, img *goquery.Selection) {
			c.changeRef(img, attachments, downloads)
		})

		title := c.mailTitle(mail)
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

func (c *EmailToHTML) mkdirs() error {
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

func (c *EmailToHTML) downloadImages(doc *goquery.Document) map[string]string {
	downloads := make(map[string]string)
	downloadLinks := make([]string, 0)
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

		downloads[src] = localFile
		downloadLinks = append(downloadLinks, src)
	})

	var batch = semaphore.NewWeighted(3)
	var group errgroup.Group

	for i := range downloadLinks {
		_ = batch.Acquire(context.TODO(), 1)

		src := downloadLinks[i]
		group.Go(func() error {
			defer batch.Release(1)

			if c.Verbose {
				log.Printf("fetch %s", src)
			}

			err := c.download(downloads[src], src)
			if err != nil {
				log.Printf("download %s fail: %s", src, err)
			}

			return nil
		})
	}

	_ = group.Wait()

	return downloads
}
func (c *EmailToHTML) extractAttachments(mail *email.Email) (attachments map[string]string, err error) {
	attachments = make(map[string]string)
	for i, a := range mail.Attachments {
		if c.Verbose {
			log.Printf("extract %s", a.Filename)
		}

		saveFile := filepath.Join(c.AttachmentsDir, fmt.Sprintf("%d.%s", i, a.Filename))
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
func (c *EmailToHTML) download(path string, src string) (err error) {
	timeout, cancel := context.WithTimeout(context.TODO(), time.Minute*2)
	defer cancel()

	info, err := os.Stat(path)
	if err == nil {
		headReq, headErr := http.NewRequestWithContext(timeout, http.MethodHead, src, nil)
		if headErr != nil {
			return headErr
		}
		resp, headErr := c.client.Do(headReq)
		if headErr == nil && info.Size() == resp.ContentLength {
			return // skip download
		}
	}

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return
	}
	defer file.Close()

	request, err := http.NewRequestWithContext(timeout, http.MethodGet, src, nil)
	if err != nil {
		return
	}
	response, err := c.client.Do(request)
	if err != nil {
		return
	}
	defer response.Body.Close()

	var written int64
	if c.Verbose {
		bar := progressbar.NewOptions64(response.ContentLength,
			progressbar.OptionSetTheme(progressbar.Theme{Saucer: "=", SaucerPadding: ".", BarStart: "|", BarEnd: "|"}),
			progressbar.OptionSetWidth(10),
			progressbar.OptionSpinnerType(11),
			progressbar.OptionShowBytes(true),
			progressbar.OptionShowCount(),
			progressbar.OptionSetPredictTime(false),
			progressbar.OptionSetDescription(filepath.Base(src)),
			progressbar.OptionSetRenderBlankState(true),
			progressbar.OptionClearOnFinish(),
		)
		defer bar.Clear()
		written, err = io.Copy(io.MultiWriter(file, bar), response.Body)
	} else {
		written, err = io.Copy(file, response.Body)
	}

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("response status code %d invalid", response.StatusCode)
	}

	if err == nil && written < response.ContentLength {
		err = fmt.Errorf("expected %s but downloaded %s", humanize.Bytes(uint64(response.ContentLength)), humanize.Bytes(uint64(written)))
	}

	return
}

func (_ *EmailToHTML) mailTitle(mail *email.Email) string {
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
