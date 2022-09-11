// Copyright 2017-2022 mikan.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/dghubble/go-twitter/twitter"
	"github.com/dghubble/oauth1"
)

const saveFile = ".jaistbot.log"
const japanesePage = "https://www.jaist.ac.jp/whatsnew/"

//const englishPage = "https://www.jaist.ac.jp/english/whatsnew/"

const prefix = "【ニュース】"
const suffix = " #JAIST"

type Entry struct {
	Title string
	URL   string
}

func main() {
	consumerKey := flag.String("ck", "", "Twitter Consumer Key")
	consumerSecret := flag.String("cs", "", "Twitter Consumer Secret")
	accessToken := flag.String("at", "", "Twitter Access Token")
	accessSecret := flag.String("as", "", "Twitter Access Secret")
	saveFilePath := flag.String("f", UserHomeDir()+saveFile, "path to save file")
	dryRun := flag.Bool("d", false, "dry run")
	flag.Parse()
	if *consumerKey == "" || *consumerSecret == "" || *accessToken == "" || *accessSecret == "" {
		log.Fatal("Consumer key/secret and Access token/secret required")
	}
	config := oauth1.NewConfig(*consumerKey, *consumerSecret)
	token := oauth1.NewToken(*accessToken, *accessSecret)
	fetched := FetchEntries(japanesePage)
	fmt.Printf("Fetched entries: %d\n", len(fetched))
	newEntries := NotYetTweeted(fetched, *saveFilePath)
	fmt.Printf("New entries:     %d\n", len(newEntries))
	Reverse(newEntries)
	for _, entry := range newEntries {
		msg := prefix + entry.Title + suffix + " " + entry.URL
		fmt.Println(msg)
		if !*dryRun {
			Tweet(config, token, msg)
		}
	}
	SaveTweeted(newEntries, *saveFilePath)
}

func FetchEntries(url string) []Entry {
	resp, err := http.Get(url)
	if err != nil {
		log.Fatalf("%s: %v\n", url, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("failed to close %s: %v", url, err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("%s: HTTP %s", url, resp.Status)
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		log.Printf("%s: %v\n", url, err)
		return nil
	}
	entries := make([]Entry, 0)
	for _, anchor := range doc.Find("#news_block").Find("a").Nodes {
		var entry Entry
		for _, attr := range anchor.Attr {
			switch attr.Key {
			case "title":
				entry.Title = attr.Val
			case "href":
				entry.URL = attr.Val
			}
		}
		if len(entry.Title) > 0 && len(entry.URL) > 0 {
			entries = append(entries, entry)
		}
	}
	return entries
}

func NotYetTweeted(fetched []Entry, path string) []Entry {
	_, err := os.Stat(path)
	if err != nil {
		f, _ := os.Create(path)
		if err := f.Close(); err != nil {
			log.Printf("Failed to close %s: %v", path, err)
		}
	}
	file, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Fatalf("Failed to close %s: %v", path, err)
		}
	}()
	scanner := bufio.NewScanner(file)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	notYetTweeted := make([]Entry, 0)
	for _, entry := range fetched {
		tweeted := false
		for _, line := range lines {
			if entry.URL == line {
				tweeted = true
				break
			}
		}
		if !tweeted {
			notYetTweeted = append(notYetTweeted, entry)
		}
	}
	return notYetTweeted
}

func SaveTweeted(tweeted []Entry, path string) {
	if len(tweeted) == 0 {
		return // skip
	}
	data := ""
	for _, entry := range tweeted {
		data = data + entry.URL + "\n"
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	defer func() {
		if err := f.Close(); err != nil {
			log.Fatalf("Failed to close %s: %v", path, err)
		}
	}()
	if err != nil {
		log.Fatal(err)
	}
	if _, err := f.WriteString(data); err != nil {
		log.Fatalf("failed to write %s: %v", path, err)
	}
}

func Tweet(config *oauth1.Config, token *oauth1.Token, status string) {
	escaped := strings.ReplaceAll(status, "\"", "”")
	escaped = strings.ReplaceAll(escaped, "@", "@ ")
	httpClient := config.Client(oauth1.NoContext, token)
	client := twitter.NewClient(httpClient)
	tweet, _, err := client.Statuses.Update(escaped, nil)
	if err != nil {
		log.Fatalf("Tweet error: %v\n", err)
	}
	fmt.Println(tweet)
}

func Reverse(entries []Entry) {
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
}

func UserHomeDir() string {
	if runtime.GOOS == "windows" {
		home := os.Getenv("HOMEDRIVE") + os.Getenv("HOMEPATH")
		if home == "" {
			home = os.Getenv("USERPROFILE")
		}
		return home + "\\"
	}
	return os.Getenv("HOME") + "/"
}
