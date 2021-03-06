package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	LoginTokenRegex   = regexp.MustCompile(`input.*token.*value="([a-zA-Z0-9]+?)"`)
	LoginExpiresRegex = regexp.MustCompile(`input.*expires.*value="([0-9]+?)"`)
)

func fetchCrossword(user, pass string, t time.Time) ([]byte, error) {
	jar, err := cookiejar.New(nil) // not secure, but that doesn't matter here
	if err != nil {
		return nil, err
	}

	client := &http.Client{Jar: jar}

	// Get the login (csrf?) token
	log.Println("Getting login page")
	resp, err := client.Get("https://myaccount.nytimes.com/auth/login")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// This is sloppy and lazy and not robust. I'll fix that, umm...later. Maybe
	// when someone ports Beautiful Soup to Go.
	lines := bytes.Split(body, []byte("\n"))
	token := ""
	expires := ""
	for _, line := range lines {
		match := LoginTokenRegex.FindSubmatch(line)
		if match != nil {
			token = string(match[1])
		}
		match = LoginExpiresRegex.FindSubmatch(line)
		if match != nil {
			expires = string(match[1])
		}
		if token != "" && expires != "" {
			break
		}
	}

	if token == "" {
		return nil, fmt.Errorf("failed to extract token from response %v", resp)
	}

	if expires == "" {
		return nil, fmt.Errorf("failed to extract expires from response %v", resp)
	}

	log.Println("Got token: ", token)
	log.Println("Got expires: ", expires)

	// Actually log in
	log.Println("Sending login credentials")
	form := url.Values{
		"token":       {token},
		"userid":      {user},
		"password":    {pass},
		"remember":    {"true"},
		"is_continue": {"false"},
		"expires":     {expires},
	}
	resp, err = client.PostForm("https://myaccount.nytimes.com/auth/login", form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Fetch the crossword
	xworddate := t.Format("Jan0206")
	log.Printf("Fetching %s.pdf\n", xworddate)
	resp, err = client.Get(fmt.Sprintf("https://www.nytimes.com/svc/crosswords/v2/puzzle/print/%s.pdf", xworddate))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Fetch failed with %d", resp.StatusCode)
	}

	contentType := resp.Header["Content-Type"]
	if len(contentType) == 0 || contentType[0] != "application/pdf" {
		return nil, fmt.Errorf("Expected content type application/pdf, got %v", contentType)
	}

	return body, nil
}

func printCrossword(path string, printer string) error {
	cmd := exec.Command("/usr/bin/lpr", "-P", printer, "-o", "fit-to-page", path)
	log.Println("Running ", cmd)
	return cmd.Run()
}

func process(user, pass, dir string, t time.Time) error {
	pdf, err := fetchCrossword(user, pass, t)
	if err != nil {
		return fmt.Errorf("Fetch failed: %v\n", err)
	}

	xworddate := time.Now().Format("Jan0206")
	path := filepath.Join(dir, xworddate+".pdf")
	log.Printf("Saving PDF to %v (size %d)\n", path, len(pdf))
	if err := ioutil.WriteFile(path, pdf, 0644); err != nil {
		return fmt.Errorf("Save to disk failed: %v\n", err)
	}

	if *fetchOnly {
		return nil
	}
	if err := printCrossword(path, "Brother"); err != nil {
		return fmt.Errorf("Printing failed: %v\n", err)
	}

	return nil
}

// parseDate parses a YYYY.MM.DD date provided
// on the command line. If s is not parseable,
// parseDate logs an error and calls os.Exit(2).
func parseDate(s string) time.Time {
	date, err := time.Parse("2006.01.02", s)
	if err != nil {
		log.Fatalf("Could not parse %s: %v", s, err)
		os.Exit(2)
	}
	return date
}

var (
	user      = flag.String("u", os.Getenv("NYT_USER"), "nyt username")
	pass      = flag.String("p", os.Getenv("NYT_PASS"), "nyt password")
	dir       = flag.String("d", os.Getenv("NYT_CROSSWORD_DIR"), "directory to save crosswords to; will not be created")
	skip      = flag.Bool("s", false, "skip today's crossword")
	one       = flag.String("o", "", "print this one date and exit, format YYYY.MM.DD")
	dateRange = flag.String("r", "", "print this range of dates (inclusive) and exit, format YYYY.MM.DD:YYYY.MM.DD")
	fetchOnly = flag.Bool("f", false, "fetch the crossword but do not print it")
)

func main() {
	flag.Parse()

	if *user == "" || *pass == "" {
		log.Printf("must provide a username and password, either on the command line or via NYT_[USER|PASS] envvars")
		flag.Usage()
		os.Exit(2)
	}

	if *one != "" {
		date := parseDate(*one)
		y, m, d := date.Date()
		log.Printf("Processing %d.%d.%d\n", y, m, d)
		if err := process(*user, *pass, *dir, date); err != nil {
			log.Fatalf("Failed: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	if *dateRange != "" {
		dates := strings.SplitN(*dateRange, ":", 2)
		if len(dates) != 2 {
			log.Println("date range must be of the form YYYY.MM.DD:YYYY.MM.DD")
			os.Exit(2)
		}
		from, to := parseDate(dates[0]), parseDate(dates[1])
		if to.Before(from) {
			from, to = to, from
		}
		fromy, fromm, fromd := from.Date()
		toy, tom, tod := to.Date()
		log.Println("printing date range", fromy, fromm, fromd, "to", toy, tom, tod)
		for from.Before(to) || from.Equal(to) {
			y, m, d := from.Date()
			log.Printf("Processing %d.%d.%d\n", y, m, d)
			if err := process(*user, *pass, *dir, from); err != nil {
				log.Fatalf("Failed: %v\n", err)
				os.Exit(1)
			}
			from = from.Add(time.Hour * 24)
		}
		os.Exit(0)
	}

	lasty, lastm, lastd := 0, time.Month(0), 0
	if *skip {
		lasty, lastm, lastd = time.Now().Date()
	}

	for {
		now := time.Now()
		y, m, d := now.Date()

		if y == lasty && m == lastm && d == lastd {
			time.Sleep(time.Hour * 1)
			log.Printf("Already printed %d.%d.%d, sleeping...\n", lasty, lastm, lastd)
			continue
		}

		err := process(*user, *pass, *dir, now)
		if err != nil {
			log.Println("Error processing %d.%d.%d: %v", y, m, d, err)
			time.Sleep(time.Minute * 15)
			continue
		}

		lasty, lastm, lastd = y, m, d
	}
}
