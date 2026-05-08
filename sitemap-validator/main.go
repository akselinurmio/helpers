package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const sitemapNS = "http://www.sitemaps.org/schemas/sitemap/0.9"

// tChangeFreq enum per sitemap XSD
var validChangefreq = map[string]bool{
	"always": true, "hourly": true, "daily": true,
	"weekly": true, "monthly": true, "yearly": true, "never": true,
}

// tLastmod: xs:date | xs:dateTime (W3C datetime format)
var lastmodFormats = []string{
	"2006-01-02",
	"2006-01-02Z07:00",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04:05.999999999",
	time.RFC3339,
	time.RFC3339Nano,
}

func validLastmod(s string) bool {
	for _, f := range lastmodFormats {
		if _, err := time.Parse(f, s); err == nil {
			return true
		}
	}
	return false
}

// tLoc / tLocSitemap: xs:anyURI, minLength=12, maxLength=2048
func validLoc(s string) error {
	l := len(s)
	if l < 12 {
		return fmt.Errorf("loc too short (%d chars, min 12)", l)
	}
	if l > 2048 {
		return fmt.Errorf("loc too long (%d chars, max 2048)", l)
	}
	if _, err := url.ParseRequestURI(s); err != nil {
		return fmt.Errorf("loc not a valid URI: %v", err)
	}
	return nil
}

// tPriority: xs:decimal, minInclusive=0.0, maxInclusive=1.0
func validPriority(s string) bool {
	f, err := strconv.ParseFloat(s, 64)
	return err == nil && f >= 0.0 && f <= 1.0
}

type urlEntry struct {
	Loc        string `xml:"loc"`
	Lastmod    string `xml:"lastmod"`
	Changefreq string `xml:"changefreq"`
	Priority   string `xml:"priority"`
}

type smEntry struct {
	Loc     string `xml:"loc"`
	Lastmod string `xml:"lastmod"`
}

type xmlURLSet struct {
	XMLName xml.Name   `xml:"urlset"`
	URLs    []urlEntry `xml:"url"`
}

type xmlSitemapIndex struct {
	XMLName  xml.Name  `xml:"sitemapindex"`
	Sitemaps []smEntry `xml:"sitemap"`
}

type walker struct {
	hasErrors bool
	seen      map[string]struct{}
}

func (w *walker) errf(source, msg string) {
	fmt.Fprintln(os.Stderr, source+": "+msg)
	w.hasErrors = true
}

func fetch(rawURL string) ([]byte, error) {
	resp, err := http.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "xml") {
		return nil, fmt.Errorf("unexpected content-type %q", ct)
	}
	return io.ReadAll(resp.Body)
}

func rootElement(data []byte) (xml.Name, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return xml.Name{}, err
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se.Name, nil
		}
	}
}

func (w *walker) validateURLSet(source string, data []byte) {
	var s xmlURLSet
	if err := xml.Unmarshal(data, &s); err != nil {
		w.errf(source, err.Error())
		return
	}
	if len(s.URLs) == 0 {
		w.errf(source, "urlset contains no <url> elements (minOccurs=1)")
		return
	}
	for i, u := range s.URLs {
		ref := fmt.Sprintf("url[%d]", i+1)
		if u.Loc == "" {
			w.errf(source, ref+": missing <loc>")
		} else if err := validLoc(u.Loc); err != nil {
			w.errf(source, ref+": "+err.Error())
		}
		if u.Lastmod != "" && !validLastmod(u.Lastmod) {
			w.errf(source, fmt.Sprintf("%s: invalid <lastmod> %q (want xs:date or xs:dateTime)", ref, u.Lastmod))
		}
		if u.Changefreq != "" && !validChangefreq[u.Changefreq] {
			w.errf(source, fmt.Sprintf("%s: invalid <changefreq> %q", ref, u.Changefreq))
		}
		if u.Priority != "" && !validPriority(u.Priority) {
			w.errf(source, fmt.Sprintf("%s: invalid <priority> %q (must be 0.0–1.0)", ref, u.Priority))
		}
	}
}

func (w *walker) validateSitemapIndex(source string, data []byte) {
	var idx xmlSitemapIndex
	if err := xml.Unmarshal(data, &idx); err != nil {
		w.errf(source, err.Error())
		return
	}
	if len(idx.Sitemaps) == 0 {
		w.errf(source, "sitemapindex contains no <sitemap> elements (minOccurs=1)")
		return
	}
	for i, sm := range idx.Sitemaps {
		ref := fmt.Sprintf("sitemap[%d]", i+1)
		if sm.Loc == "" {
			w.errf(source, ref+": missing <loc>")
			continue
		}
		if err := validLoc(sm.Loc); err != nil {
			w.errf(source, ref+": "+err.Error())
		}
		if sm.Lastmod != "" && !validLastmod(sm.Lastmod) {
			w.errf(source, fmt.Sprintf("%s: invalid <lastmod> %q (want xs:date or xs:dateTime)", ref, sm.Lastmod))
		}
		w.walk(sm.Loc)
	}
}

func (w *walker) walk(rawURL string) {
	if _, visited := w.seen[rawURL]; visited {
		return
	}
	w.seen[rawURL] = struct{}{}

	data, err := fetch(rawURL)
	if err != nil {
		w.errf(rawURL, fmt.Sprintf("fetch: %v", err))
		return
	}

	root, err := rootElement(data)
	if err != nil {
		w.errf(rawURL, err.Error())
		return
	}

	if root.Space != sitemapNS {
		w.errf(rawURL, fmt.Sprintf("namespace %q, want %q", root.Space, sitemapNS))
	}

	switch root.Local {
	case "urlset":
		w.validateURLSet(rawURL, data)
	case "sitemapindex":
		w.validateSitemapIndex(rawURL, data)
	default:
		w.errf(rawURL, fmt.Sprintf("root element <%s>, want <urlset> or <sitemapindex>", root.Local))
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: sitemap-validator <sitemap-url> [<sitemap-url>...]")
		os.Exit(2)
	}
	w := &walker{seen: make(map[string]struct{})}
	for _, u := range os.Args[1:] {
		w.walk(u)
	}
	if w.hasErrors {
		os.Exit(1)
	}
}
