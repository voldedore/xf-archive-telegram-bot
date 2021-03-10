package xfbot

import (
	"context"
	"strings"
	// "encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/robfig/cron"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	tb "gopkg.in/tucnak/telebot.v2"
)

// pageParam is default XF2 param for number of page
const pageParam = "page-"

// breakDuration is set in order to prevent DOS
const breakDuration = 3

const vozUrl = "https://voz.vn"

var collection *mongo.Collection

// User is a post author
type User struct {
	ID   int64  `json:"user_id"`
	Name string `json:"user_name"`
}

// Message is a post, literally
type Message struct {
	CreatedBy *User     `json:"posted_by"`
	Time      time.Time `json:"message_time"`
	Body      string    `json:"message_body"`
	URL       string    `json:"message_url"`
	ID        int64     `json:"message_id"`
}

// Stat is what we store in the DB
type Stat struct {
	ThreadId int64 `json:"thread_id" bson:"thread_id"`
	Page     int   `json:"page" bson:"page"`
	PostId   int64 `json:"post_id" bson:"post_id"`
}

var messages = []*Message{}

func getOsEnv(variable string, required bool, defaultVal string) string {
	res := os.Getenv(variable)

	if res == "" {
		if required {
			log.Fatalln("No variable named " + variable + " found")
		} else {
			log.Panicln("No variable named " + variable + " found")
		}
		return defaultVal
	}
	return res
}

func buildVozLink(threadLink string) string {
	return vozUrl + "/t/" + threadLink
}

func buildXfLinkWithPageParam(xfThreadLink string, pageNo int) string {
	return xfThreadLink + "/" + pageParam + strconv.Itoa(pageNo)
}

func fetchMessages(threadLink string, b *tb.Bot, channel *tb.Chat) {
	threadIdStr := strings.Split(threadLink, ".")[1]
	threadId, _ := strconv.ParseInt(threadIdStr, 0, 64)
	storedPageNo, lastPostId := getLastInfo(threadId)

	if lastPostId == 0 {
		initCollection(threadId)
	}

	newestPostId, newestPage := fetchPage(threadLink, storedPageNo, lastPostId)
	if newestPostId != 0 {
		if storedPageNo < newestPage {
			for i := storedPageNo; i <= newestPage; i++ {
				newestPostId, _ = fetchPage(threadLink, i, lastPostId)
			}
		}

		// Update lastId
		log.Printf("Last Post id = %d", newestPostId)
		updateInfo(threadId, newestPage, newestPostId)

		log.Println("Publishing...")
		for _, el := range messages {
			b.Send(channel, makeMessage(threadId, el.Body, el.URL, el.Time, el.CreatedBy.Name), tb.ModeMarkdown)
		}
		messages = []*Message{}
		log.Println("End publishing...")
	}
}

func fetchPage(threadLink string, pageNo int, storedPostId int64) (int64, int) {
	doc := getDocument(buildXfLinkWithPageParam(buildVozLink(threadLink), pageNo))

	var lastId int64
	newPage := 1

	if doc != nil {
		// Post looking
		doc.Find("article.message").Each(func(i int, s *goquery.Selection) {
			postIdStr, _ := s.Attr("data-content")
			postId, _ := strconv.ParseInt(postIdStr[5:], 0, 64)
			lastId = postId

			if postId > storedPostId {
				log.Printf("%d", postId)

				userIDStr, _ := s.Find("h4.message-name a").Attr("data-user-id")
				uid, _ := strconv.ParseInt(userIDStr, 0, 64)
				user := &User{ID: uid}
				user.Name = s.Find("h4.message-name a").Text()

				message := &Message{Body: s.Find("article.message-body .bbWrapper").Text(),
					CreatedBy: user}
				messageTimeRaw, _ := s.Find(".message-attribution time").Attr("data-time")
				messageTime, _ := strconv.ParseInt(messageTimeRaw, 0, 64)
				message.Time = time.Unix(messageTime, 0)

				messagePermalink, _ := s.Find(".message-attribution a").Attr("href")
				message.URL = messagePermalink
				message.ID = postId
				messages = append(messages, message)
			}
		})

		// Page looking
		pageNav := doc.Find(".pageNav-main")
		if pageNav != nil {
			pageNavEl := pageNav.First()
			str, err := pageNavEl.Children().Last().Children().First().Html()
			if err != nil {
				log.Fatal(err)
			}
			newPage, _ = strconv.Atoi(str)
			log.Printf("Newest page: %s", str)
		}

	}
	return lastId, newPage
}

func getDB() *mongo.Collection {
	log.Println("Connecting DB...")

	dbUsername := getOsEnv("MONGODB_USERNAME", true, "")
	dbPassword := getOsEnv("MONGODB_PWD", true, "")
	dbAddress := getOsEnv("MONGODB_ADDR", true, "")
	//dbPort := getOsEnv("MONGODB_PORT", true, "27017")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(
		"mongodb+srv://"+dbUsername+":"+dbPassword+"@"+dbAddress+"/xfarchive?retryWrites=true&w=majority",
	))
	if err != nil {
		log.Fatal(err)
	}
	collection = client.Database("xfarchive").Collection("stats")
	return collection
}

func getLastInfo(threadId int64) (int, int64) {
	log.Println("Get stored information in DB")
	log.Printf("Looking for info of threadId: %d", threadId)
	filter := bson.D{{"thread_id", threadId}}
	var stat Stat
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := collection.FindOne(ctx, filter).Decode(&stat)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Page: %s, PostId: %s", strconv.Itoa(stat.Page), strconv.FormatInt(stat.PostId, 10))
	return stat.Page, stat.PostId
}

func initCollection(threadId int64) {
	log.Println("Initializing DB...")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, err := collection.InsertOne(ctx, bson.M{
		"thread_id": threadId,
		"page":      1,
		"post_id":   0,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Indexes
	opts := options.Index().SetUnique(true)
	model := mongo.IndexModel{Keys: bson.M{"thread_id": 1}, Options: opts}
	name, err := collection.Indexes().CreateOne(ctx, model)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Created index name:")
	log.Println(name)
}

func updateInfo(threadId int64, pageNo int, postId int64) {

	log.Println("Updating info...")
	opts := options.FindOneAndUpdate().SetUpsert(false)
	filter := bson.D{{"thread_id", threadId}}
	update := bson.D{{"$set", bson.D{{"post_id", postId}}}, {"$set", bson.D{{"page", pageNo}}}}
	var updatedDocument bson.M
	err := collection.FindOneAndUpdate(context.TODO(), filter, update, opts).Decode(&updatedDocument)
	if err != nil {
		// ErrNoDocuments means that the filter did not match any documents in the collection
		if err == mongo.ErrNoDocuments {
			return
		}
		log.Fatal(err)
	}
}

func makeMessage(threadId int64, title string, link string, time time.Time, author string) string {
	return fmt.Sprintf("Thread #%d\nOn %s, %s said: \n%s\nSee more: %s", threadId, time, author, title, link)
}

// getDocument accepts a thread URL, then fetches the page and returns a goQuery document
func getDocument(url string) *goquery.Document {

	client := &http.Client{}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Print(err)
		return nil
	}

	req.Header.Set("User-Agent", "telegram-bot:xfpost:v1.0.0")

	resp, err := client.Do(req)

	if err != nil {
		log.Print(err)
	} else {

		defer resp.Body.Close()

		doc, err := goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			log.Fatal(err)
		}

		log.Printf("GET %s successfully\n", url)
		return doc
	}

	return nil
}

func xfBot() {
	thread1 := getOsEnv("THREAD_1", true, "")
	b, err := tb.NewBot(tb.Settings{
		Token:  os.Getenv("NEWS_BOT_SECRET_TOKEN"),
		Poller: &tb.LongPoller{Timeout: 10 * time.Second},
	})

	if err != nil {
		log.Fatal(err)
		return
	}

	b.Handle(tb.OnText, func(m *tb.Message) {
		b.Send(m.Sender, "This bot does not currently support the interactive mode")
	})

	// Getting the Channel
	channel, channelGetErr := b.ChatByID(os.Getenv("CHANNEL_ID"))

	if channelGetErr != nil {
		log.Fatal(channelGetErr)
		return
	}

	getDB()
	fetchMessages(thread1, b, channel)

	// Cron
	// The legacy syntax (asterisks) doesn't work if this package is imported to another program
	// Although it runs well directly if this package was compile as a complete program
	c := cron.New()
	c.AddFunc("@every 5m", func() {
		fetchMessages(thread1, b, channel)
	})

	c.Start()
	// Testing purpose
}

func Bot() {
	//go xfBot()
	//select {}
	xfBot()
}
