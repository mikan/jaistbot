// Copyright 2017 mikan.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"bufio"
	"runtime"

	"github.com/PuerkitoBio/goquery"
	"github.com/dghubble/go-twitter/twitter"
	"github.com/dghubble/oauth1"
)

const logFile = ".jaistbot.log"
const japanesePage = "https://www.jaist.ac.jp/whatsnew/"

// const englishPage = "https://www.jaist.ac.jp/english/whatsnew/"
const prefix = "【ニュース】"
const suffix = " #jaist"

type Entry struct {
	Title string
	URL   string
}

func main() {
	flags := flag.NewFlagSet("jaistbot", flag.ExitOnError)
	consumerKey := flags.String("ck", "", "Twitter Consumer Key")
	consumerSecret := flags.String("cs", "", "Twitter Consumer Secret")
	accessToken := flags.String("at", "", "Twitter Access Token")
	accessSecret := flags.String("as", "", "Twitter Access Secret")
	flags.Parse(os.Args[1:])
	if *consumerKey == "" || *consumerSecret == "" || *accessToken == "" || *accessSecret == "" {
		log.Fatal("Consumer key/secret and Access token/secret required")
	}
	config := oauth1.NewConfig(*consumerKey, *consumerSecret)
	token := oauth1.NewToken(*accessToken, *accessSecret)
	fetched := GetEntries(japanesePage)
	fmt.Printf("Fetched entries: %d\n", len(fetched))
	newEntries := NotYetTweeted(fetched)
	fmt.Printf("New entries:     %d\n", len(newEntries))
	Reverse(newEntries)
	for _, entry := range newEntries {
		msg := prefix + entry.Title + suffix + " " + entry.URL
		fmt.Println(msg)
		Tweet(config, token, msg)
	}
	SaveTweeted(newEntries)
}

func GetEntries(url string) []Entry {
	doc, err := goquery.NewDocument(url)
	if err != nil {
		fmt.Errorf("%s: %v\n", url, err)
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

func NotYetTweeted(fetched []Entry) []Entry {
	_, err := os.Stat(UserHomeDir() + logFile)
	if err != nil {
		f, _ := os.Create(UserHomeDir() + logFile)
		f.Close()
	}
	file, err := os.Open(UserHomeDir() + logFile)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
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

func SaveTweeted(tweeted []Entry) {
	if len(tweeted) == 0 {
		return // skip
	}
	data := ""
	for _, entry := range tweeted {
		data = data + entry.URL + "\n"
	}
	f, err := os.OpenFile(UserHomeDir()+logFile, os.O_APPEND|os.O_WRONLY, 0644)
	defer f.Close()
	if err != nil {
		log.Fatal(err)
	}
	f.WriteString(data)
}

func Tweet(config *oauth1.Config, token *oauth1.Token, status string) {
	httpClient := config.Client(oauth1.NoContext, token)
	client := twitter.NewClient(httpClient)
	tweet, _, err := client.Statuses.Update(status, nil)
	if err != nil {
		fmt.Errorf("Tweet error: %v\n", err)
		return
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
