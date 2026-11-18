// Copyright 2017-2022 mikan.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/dghubble/oauth1"
	"github.com/g8rswimmer/go-twitter/v2"
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
	bufferAPIKey := flag.String("bk", "", "Buffer API Key")
	bufferChannelID := flag.String("bc", "", "Buffer Channel ID")
	saveFilePath := flag.String("f", UserHomeDir()+saveFile, "path to save file")
	webhook := flag.String("w", "", "webhook url for error notification")
	dryRun := flag.Bool("d", false, "dry run")
	flag.Parse()
	if *consumerKey == "" && *bufferAPIKey == "" {
		log.Fatal("Twitter credentials or Buffer API key is required")
	}
	if *consumerKey != "" && (*consumerSecret == "" || *accessToken == "" || *accessSecret == "") {
		log.Fatal("Consumer key/secret and Access token/secret is required")
	}
	if *bufferAPIKey != "" && *bufferChannelID == "" {
		log.Fatal("Buffer Channel ID is also required")
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
		msg = strings.ReplaceAll(msg, "\"", "”")
		msg = strings.ReplaceAll(msg, "@", "@ ")
		fmt.Println(msg)
		if !*dryRun {
			var err error
			if *bufferAPIKey != "" {
				err = BufferPost(*bufferAPIKey, *bufferChannelID, msg)
			} else {
				err = Tweet(config, token, msg)
			}
			if err != nil {
				if len(*webhook) > 0 {
					if wErr := IncomingWebhook(*webhook, msg, err); wErr != nil {
						log.Fatalf("failed to post webhook: %v, original error: %v", wErr, err)
					}
				}
				log.Fatal(err)
			}
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
		if !slices.Contains(lines, entry.URL) {
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

type dummyAuthorizer struct{ Token string }

func (a dummyAuthorizer) Add(_ *http.Request) {}

func Tweet(config *oauth1.Config, token *oauth1.Token, status string) error {
	client := &twitter.Client{
		Authorizer: dummyAuthorizer{},
		Client:     config.Client(oauth1.NoContext, token),
		Host:       "https://api.twitter.com",
	}
	resp, err := client.CreateTweet(context.Background(), twitter.CreateTweetRequest{Text: status})
	if err != nil {
		return fmt.Errorf("Tweet error: %w\n", err)
	}
	log.Printf("Tweeted: %s (rate limit: %d/%d)", resp.Tweet.ID, resp.RateLimit.Remaining, resp.RateLimit.Limit)
	return nil
}

func BufferPost(key string, cid string, status string) error {
	const query = `mutation CreatePost($input: CreatePostInput!) {
		createPost(input: $input) {
			... on PostActionSuccess { post { id text status } }
			... on MutationError { message }
		}
	}`
	var payload struct {
		Query     string `json:"query"`
		Variables struct {
			Input struct {
				Text           string `json:"text"`
				ChannelID      string `json:"channelId"`
				SchedulingType string `json:"schedulingType"`
				Mode           string `json:"mode"`
			} `json:"input"`
		} `json:"variables"`
	}
	payload.Query = query
	payload.Variables.Input.Text = status
	payload.Variables.Input.ChannelID = cid
	payload.Variables.Input.SchedulingType = "automatic"
	payload.Variables.Input.Mode = "shareNow"
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, "https://api.buffer.com", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("BufferPost error: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Fatalf("Failed to close Buffer response body: %v", err)
		}
	}()
	result, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read Buffer response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("BufferPost error: %s (status=%d)", string(result), resp.StatusCode)
	}
	var parsed struct {
		Data struct {
			CreatePost struct {
				Post *struct {
					ID     string `json:"id"`
					Status string `json:"status"`
				} `json:"post"`
				Message string `json:"message"`
			} `json:"createPost"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return fmt.Errorf("failed to unmarshal Buffer response: %w (body: %s)", err, string(result))
	}
	if len(parsed.Errors) > 0 {
		return fmt.Errorf("BufferPost error: %s", parsed.Errors[0].Message)
	}
	if parsed.Data.CreatePost.Message != "" {
		return fmt.Errorf("BufferPost error: %s", parsed.Data.CreatePost.Message)
	}
	post := parsed.Data.CreatePost.Post
	if post == nil {
		return fmt.Errorf("BufferPost error: no post in response (body: %s)", string(result))
	}
	log.Printf("Posted via Buffer: %s (status: %s)", post.ID, post.Status)
	return nil
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

func IncomingWebhook(url, msg string, err error) error {
	text := fmt.Sprintf("Message:\n%s\nError:\n%v", msg, err)
	payload, err := json.Marshal(struct {
		Text    string `json:"text"`    // Slack
		Content string `json:"content"` // Discord
	}{Text: text, Content: text})
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to post webhook: %w", err)
	}
	defer func() {
		if err = resp.Body.Close(); err != nil {
			log.Fatalf("Failed to close webhook body: %v", err)
		}
	}()
	result, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("webhook response: %s (status=%d)", string(result), resp.StatusCode)
	}
	return nil
}
