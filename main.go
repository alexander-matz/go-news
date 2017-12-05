package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log"
	_ "net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const perPage int = 25
const dbFile string = "./data.bolt"
const configFile string = "./config.json"

var logger *log.Logger

func loadHTMLGlob(engine *gin.Engine, pattern string, urlfunc func(string) string) {
	funcMap := template.FuncMap{
		"url":    urlfunc,
		"hashID": HashID,
		"date": func(t time.Time) string {
			return t.Format("2006-01-02 15:04 -0700")
		},
		"when": func(t time.Time) string {
			return DurationToHuman(t.UTC().Sub(time.Now().UTC()))
		},
		"lastPost": func(posts []*Post) *Post {
			if len(posts) > 0 {
				return posts[len(posts)-1]
			} else {
				return nil
			}
		},
	}
	templ := template.Must(template.New("").Funcs(funcMap).ParseGlob(pattern))
	engine.SetHTMLTemplate(templ)
}

type App struct {
	BaseURL string `json:"baseUrl,omitempty"`
	Address string `json:"address,omitempty"`
	Passwd  string `json:"passwd,omitempty"`
	Mode    string `json:"mode,omitempty"`
	DBFile  string `json:"dbfile,omitempty"`
	CfgFile string `json:"-"`
	PerPage int    `json:"perpage,omitempty"`
}

var Default = App{
	BaseURL: "",
	Address: ":8080",
	Passwd:  "",
	Mode:    "debug",
	DBFile:  "./data.bolt",
	CfgFile: "./config.json",
	PerPage: 25,
}

func loadConfig(filename string, app *App) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	err = decoder.Decode(app)
	if err != nil {
		return err
	}
	return nil
}

func cmdRun() error {
	app := Default

	err := loadConfig(app.CfgFile, &app)
	if err != nil {
		return err
	}
	if app.Passwd == "" {
		return errors.New("NO PASSWORD SPECIFIED")
	}

	switch app.Mode {
	case "debug":
		gin.SetMode(gin.DebugMode)
	case "release":
		gin.SetMode(gin.ReleaseMode)
	}

	url := func(url string) string {
		return app.BaseURL + url
	}

	/* START SUBSYSTEMS */

	store, err := NewStore(app.DBFile, NewPrefixedLogger("store"))
	if err != nil {
		return err
	}
	defer store.Close()

	if store.CheckVersion() != "0.2" {
		return errors.New("old database format")
	}

	feedd := NewFeedD(store, NewPrefixedLogger("feedd"))
	feedd.Start()
	defer feedd.Stop()

	stats := NewStats(NewPrefixedLogger("stats"))
	stats.Start()
	defer stats.Stop()

	/* PERIODICALLY TRIM ARTICLES */
	stoptrim := make(chan bool, 1)
	go func(stop chan bool) {
		for true {
			store.PostsTrim()
			select {
			case <-stop:
				return
			case <-time.After(time.Minute * 60):
				break
			}
		}
	}(stoptrim)
	defer func() {
		stoptrim <- true
	}()

	/* CONFIGURE SERVER */
	r := gin.Default()

	loadHTMLGlob(r, "./templates/*", url)

	//AddPprof(r)

	r.Static(url("/static"), "./static")

	/*   /   - INDEX */

	r.GET(url("/"), func(c *gin.Context) {
		sitemap := make(map[string]string)
		sitemap["/f/"] = "show all feeds"
		sitemap["/f/bbc+wik"] = "show only feeds BBC and Wiki News"
		sitemap["/l/"] = "list available feeds"
		sitemap["/r/"] = "request a feed to be added"
		//sitemap["/i/"] = "statistics"
		c.HTML(200, "index.tmpl", gin.H{"sitemap": sitemap})
	})

	/*   /f/ - NEWS */

	r.GET(url("/f/"), func(c *gin.Context) {
		after := c.Query("after")
		path := c.Request.URL.Path
		feedsMap := store.FeedsAllMap()
		var refID int64
		if after == "" {
			refID = MakeIDRaw(time.Now(), 0, 0)
		} else {
			refID = UnhashID(after)
		}
		posts := store.PostsFilter(app.PerPage, func(p *Post) bool {
			return p.ID < refID
		})
		c.HTML(200, "posts.tmpl",
			gin.H{"posts": posts, "feeds": feedsMap, "path": path})
	})
	r.GET(url("/f/:feeds"), func(c *gin.Context) {
		after := c.Query("after")
		path := c.Request.URL.Path
		feedsMap := store.FeedsAllMap()
		var refID int64
		if after == "" {
			refID = MakeIDRaw(time.Now(), 0, 0)
		} else {
			refID = UnhashID(after)
		}
		feedsLookup := make(map[string]bool)
		for _, feed := range strings.Split(c.Param("feeds"), "+") {
			feedsLookup[feed] = true
		}
		posts := store.PostsFilter(app.PerPage, func(p *Post) bool {
			feed := feedsMap[p.Feed].Handle
			_, ok := feedsLookup[feed]
			return p.ID < refID && ok
		})
		c.HTML(200, "posts.tmpl",
			gin.H{"posts": posts, "feeds": feedsMap, "path": path})
	})

	/*   /l/ - FEED LIST */

	r.GET(url("/l/"), func(c *gin.Context) {
		feeds := store.FeedsAll()
		c.HTML(200, "feeds.tmpl", gin.H{"feeds": feeds})
	})

	/*   /a/*- ARTICLES */

	r.GET(url("/a/:articleid"), func(c *gin.Context) {
		postID := UnhashID(c.Param("articleid"))
		if err != nil {
			c.String(200, "Internal error")
			return
		}
		post := store.PostsGet(postID)
		if post == nil {
			c.String(200, fmt.Sprintf("invalid article: %s", HashID(postID)))
			return
		}
		feedsMap := store.FeedsAllMap()
		feed := feedsMap[post.Feed]
		r, err := store.ReadabilityGetOne(post.ID)
		if err != nil {
			c.String(200, err.Error())
			return
		}
		c.HTML(200, "article.tmpl",
			gin.H{"post": post, "content": template.HTML(r.Content), "feed": feed})
	})

	/*   /r/ - FEED REQUESTS */

	r.GET(url("/r/"), func(c *gin.Context) {
		requests, err := store.FeedReqsAll()
		if err != nil {
			c.String(200, "Internal error")
			return
		}
		_ = sort.IsSorted(FeedReqsByCount(requests))
		sort.Reverse(FeedReqsByCount(requests))
		c.HTML(200, "requests.tmpl", gin.H{"requests": requests})
	})
	r.POST(url("/r/"), func(c *gin.Context) {
		reqURL := strings.Trim(c.PostForm("feedurl"), " \t\n\r\f")
		if !ValidateURL(reqURL) {
			c.String(200, "malformed feed request url")
			return
		}
		store.FeedReqsAdd(reqURL)
		if err != nil {
			c.String(200, "Internal error")
			return
		}
		c.Redirect(303, app.BaseURL+"/r/")
	})

	/*   /c/*- CONTROL CENTER */

	r.GET(url("/c/"), func(c *gin.Context) {
		userpw := c.Query("passwd")
		if subtle.ConstantTimeCompare([]byte(userpw), []byte(app.Passwd)) == 0 {
			c.String(200, "access denied")
			return
		}
		stats := make(map[string]interface{})
		stats["numFeeds"] = len(store.FeedsAll())
		stats["numPosts"] = len(store.PostsFilter(-1, func(*Post) bool { return true }))
		c.HTML(200, "control.tmpl", gin.H{"stats": stats})
	})

	r.POST(url("/c/"), func(c *gin.Context) {
		userpw := c.Query("passwd")
		if subtle.ConstantTimeCompare([]byte(userpw), []byte(app.Passwd)) == 0 {
			c.String(200, "access denied")
			return
		}
		key := c.PostForm("cmd")
		val := c.PostForm("arg")
		c.String(200, key+" = "+val)
	})

	/*   /x/*- APIs */

	r.GET(url("/x/r/:articleid"), func(c *gin.Context) {
		postID := UnhashID(c.Param("articleid"))
		if err != nil {
			c.String(200, err.Error())
			return
		}
		post := store.PostsGet(postID)
		if post == nil {
			c.String(200, fmt.Sprintf("invalid article: %d", postID))
			return
		}
		r, err := store.ReadabilityGetOne(post.ID)
		if err != nil {
			c.String(200, err.Error())
			return
		}
		js, err := json.Marshal(r)
		if err != nil {
			c.String(200, err.Error())
			return
		}
		c.String(200, string(js))
	})

	r.Run(app.Address)

	return nil
}

func cmdCmd() error {
	flag.Parse()
	if flag.NArg() < 2 {
		return help()
	}
	return errors.New("Not yet implemented")
}

type command struct {
	name string
	desc string
	fun  func() error
}

var commands []*command

func help() error {
	log.Printf("Usage:")
	log.Printf("    go-news command")
	log.Printf("")
	log.Printf("Commands are:")
	var longest int
	for _, cmd := range commands {
		if len(cmd.name) > longest {
			longest = len(cmd.name)
		}
	}

	for _, cmd := range commands {
		log.Printf("    % -*s  %s", longest, cmd.name, cmd.desc)
	}
	return nil
}

func doCommand(name string) error {
	for _, cmd := range commands {
		if cmd.name == name {
			return cmd.fun()
		}
	}
	return errors.New("unknown command: " + name)
}

func addCommand(fun func() error, name, desc string) {
	if commands == nil {
		commands = make([]*command, 0)
	}
	commands = append(commands, &command{name, desc, fun})
}

func main() {
	addCommand(cmdRun, "run", "start service regularly")
	addCommand(cmdInit, "init", "initialize empty database")
	addCommand(cmdInitDefaults, "initdef", "initialize database with defaults")
	addCommand(cmdCmd, "cmd", "send a command to a running service")

	addCommand(cmdTest, "test", "unspecified tests for development")

	addCommand(cmdUpdateDB, "updatedb", "migrate database to current format/fix known bugs")

	if len(os.Args) < 2 {
		help()
		return
	}

	HashIDInit()
	logger = NewPrefixedLogger("main")
	ValidateURL("www.google.de")

	if err := doCommand(os.Args[1]); err != nil {
		logger.Fatal(err.Error())
	}
}
