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
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

var (
	LoginTokenRegex = regexp.MustCompile(`input.*token.*value="([a-zA-Z0-9]+?)"`)
)

// date formatted like Aug0513
func fetchCrossword(user, pass, date string) ([]byte, error) {
	jar, err := cookiejar.New(nil) // not secure, but that doesn't matter here
	if err != nil {
		return nil, err
	}

	client := &http.Client{Jar: jar}

	// Get the login (csrf?) token
	log.Println("Getting login page")
	loginTime := time.Now()
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
	for _, line := range lines {
		match := LoginTokenRegex.FindSubmatch(line)
		if match != nil {
			token = string(match[1])
			break
		}
	}

	if token == "" {
		return nil, fmt.Errorf("failed to extract token from response %v", resp)
	}

	log.Println("Got token %v", token)

	// Actually log in
	log.Println("Sending login credentials")
	expires := loginTime.Add(-time.Minute * 45).Unix()
	resp, err = client.PostForm("https://myaccount.nytimes.com/auth/login", url.Values{
		"token":       {token},
		"userid":      {user},
		"password":    {pass},
		"remember":    {"false"},
		"is_continue": {"false"},
		"expires":     {strconv.Itoa(int(expires))},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Fetch the crossword
	log.Println("Fetching PDF")
	// TODO: Convert time into a date string to insert here
	resp, err = client.Get(fmt.Sprintf("http://select.nytimes.com/premium/xword/2013/08/19/%s.pdf", date))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func main() {
	user := flag.String("u", os.Getenv("NYT_USER"), "nyt username")
	pass := flag.String("p", os.Getenv("NYT_PASS"), "nyt password")
	dir := flag.String("d", os.Getenv("NYT_CROSSWORD_DIR"), "directory to save crosswords to; will not be created")

	flag.Parse()

	if *user == "" || *pass == "" {
		log.Printf("must provide a username and password, either on the command line or via NYT_[USER|PASS] envvars")
		flag.Usage()
		os.Exit(2)
	}

	date := "Aug1913"
	pdf, err := fetchCrossword(*user, *pass, date, time.Now())
	if err != nil {
		log.Fatal(err)
	}

	path = filepath.Join(*dir, date+".pdf")
	log.Println("Saving PDF to %v (size %d)", path, len(body))
	err = ioutil.WriteFile(path, pdf, 0644)
	if err != nil {
		log.Fatal(err)
	}

	return nil
}
