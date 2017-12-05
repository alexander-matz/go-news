package main

import (
    "time"
    "log"
    "os"
    "sort"
    "fmt"
    "strings"
    "html/template"
    "encoding/json"
    "errors"
    _ "crypto/subtle"
    _ "net/url"
	"net/http/pprof"

    "gopkg.in/alecthomas/kingpin.v2"
    "github.com/gin-gonic/gin"
	"regexp"
    )


var (
    logger *log.Logger

    // command line interface
    app = kingpin.New("go-news", "A less distracting RSS reader.")
    appDbPath = app.Flag("db-path", "Path to the database file.").Short('d').Default("./data.bolt").String()
    appDebug  = app.Flag("debug", "Enable debug mode.").Default("false").Bool()
    //configPath = app.Flag("config-path", "Path to the config file.").Short('c').Default("./config.json").File()

    serve = app.Command("serve", "Run the server.")
    serveBaseUrl     = serve.Flag("base-url", "Required if go-news runs in a subdirectory.").Short('b').Default("").String()
    serveBindAddress = serve.Flag("address", "Binding Address.").Short('a').Default(":8080").String()
    servePerPage     = serve.Flag("per-page", "News items per page.").Default("25").Int()
    serveProfile     = serve.Flag("profile", "Enable profiling.").Default("false").Bool()

    add = app.Command("add", "Add something.")
    addFeed = add.Command("feed", "Add a feed.")
    addFeedHandle  = addFeed.Arg("handle", "Handle the feed should be identified by.").Required().String()
    addFeedAddress = addFeed.Arg("address", "Address of the RSS feed to add.").Required().String()
    addRequest = add.Command("request", "Add a feed request (not implemented)")

	del = app.Command("delete", "Delete something.")
	delFeed = del.Command("feed", "Delete a feed.")
	delFeedHandleOrAddress = delFeed.Arg("handle-or-address", "Handle or address of the feed to delete").Required().String()

	clear = app.Command("clear", "Clear something.")
	clearRequest = clear.Command("requests", "Clear all feed requests.")

	list = app.Command("list", "List something.")
	listFeeds = list.Command("feeds", "List all feeds.")

    initialize = app.Command("init", "Initialize the database.")
    initializeEmpty = initialize.Command("empty", "Initialize the database as empty.")

    migrate = app.Command("migrate", "Migrate database from old format.")

    // embedded services
    store *Store = nil
    feedd *FeedD = nil
    stats *Stats = nil

	// regexps
	handleRE = regexp.MustCompile("[a-zA-Z][a-zA-Z0-9]*")
)

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

func cmdRun() error {
    // START STORAGE SERVICE

    store, err := NewStore(*appDbPath, NewPrefixedLogger("store"))
    if err != nil {
        return err;
    }
    defer store.Close()

    if store.CheckVersion() != "0.2" {
        return errors.New("old database format")
    }

    // START FEED CRAWLER

    feedd := NewFeedD(store, NewPrefixedLogger("feedd"))
    feedd.Start()
    defer feedd.Stop()

    // START STATISTICS COLLECTION

    stats := NewStats(NewPrefixedLogger("stats"))
    stats.Start()
    defer stats.Stop()

    // START TRIMMER

    stoptrim := make(chan bool, 1)
    go func (stop chan bool) {
        for true {
            store.PostsTrim()
            select {
            case <-stop:
                return;
            case <-time.After(time.Minute * 60):
                break;
            }
        }
    }(stoptrim)
    defer func() {
        stoptrim <- true
    }()

    // CONFIGURE SERVER

    url := func(url string) string {
        return *serveBaseUrl + url
    }

    r := gin.Default()

    if *appDebug {
        gin.SetMode(gin.DebugMode)
    } else {
        gin.SetMode(gin.ReleaseMode)
    }

    loadHTMLGlob(r, "./templates/*", url)

    // CONFIGURE ROUTES

    r.Static(url("/static"), "./static")

    // profiler routes, only add in debug
    if *appDebug {
        r.GET("/debug/pprof/",func(ctx *gin.Context) {pprof.Index(ctx.Writer, ctx.Request)})
        r.GET("/debug/pprof/heap", func(ctx *gin.Context) {pprof.Handler("heap").ServeHTTP(ctx.Writer, ctx.Request)})
        r.GET("/debug/pprof/goroutine", func(ctx *gin.Context) {pprof.Handler("goroutine").ServeHTTP(ctx.Writer, ctx.Request)})
        r.GET("/debug/pprof/block", func(ctx *gin.Context) {pprof.Handler("block").ServeHTTP(ctx.Writer, ctx.Request)})
        r.GET("/debug/pprof/threadcreate", func(ctx *gin.Context) {pprof.Handler("threadcreate").ServeHTTP(ctx.Writer, ctx.Request)})
        r.GET("/debug/pprof/cmdline", func(ctx *gin.Context) {pprof.Cmdline(ctx.Writer, ctx.Request)})
        r.GET("/debug/pprof/profile", func(ctx *gin.Context) {pprof.Profile(ctx.Writer, ctx.Request)})
        r.GET("/debug/pprof/symbol",  func(ctx *gin.Context) {pprof.Symbol(ctx.Writer, ctx.Request)})
        r.POST("/debug/pprof/symbol", func(ctx *gin.Context) {pprof.Symbol(ctx.Writer, ctx.Request)})
        r.GET("/debug/pprof/trace", func(ctx *gin.Context) {pprof.Trace(ctx.Writer, ctx.Request)})
    }

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
        posts := store.PostsFilter(*servePerPage, func(p *Post) bool {
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
        for _, feed := range(strings.Split(c.Param("feeds"), "+")) {
            feedsLookup[feed]= true
        }
        posts := store.PostsFilter(*servePerPage, func(p *Post) bool {
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
        if err != nil { c.String(200, "Internal error"); return }
        post := store.PostsGet(postID)
        if post == nil { c.String(200, fmt.Sprintf("invalid article: %s", HashID(postID))); return }
        feedsMap := store.FeedsAllMap()
        feed := feedsMap[post.Feed];
        r, err := store.ReadabilityGetOne(post.ID)
        if err != nil { c.String(200, err.Error()); return }
        c.HTML(200, "article.tmpl",
            gin.H{"post": post, "content": template.HTML(r.Content), "feed": feed})
    })

    /*   /r/ - FEED REQUESTS */

    r.GET(url("/r/"), func (c *gin.Context) {
        requests, err := store.FeedReqsAll()
        if err != nil { c.String(200, "Internal error"); return }
        _ = sort.IsSorted(FeedReqsByCount(requests))
        sort.Reverse(FeedReqsByCount(requests))
        c.HTML(200, "requests.tmpl", gin.H{"requests": requests})
    })
    r.POST(url("/r/"), func (c *gin.Context) {
        reqURL := strings.Trim(c.PostForm("feedurl"), " \t\n\r\f")
        if ! ValidateURL(reqURL) {
            c.String(200, "malformed feed request url")
            return
        }
        store.FeedReqsAdd(reqURL)
        if err != nil { c.String(200, "Internal error"); return }
        c.Redirect(303, url("/r/"))
    })

    /*   /c/*- CONTROL CENTER */

    /*
    r.GET(url("/c/"), func(c *gin.Context) {
        userpw := c.Query("passwd")
        if subtle.ConstantTimeCompare([]byte(userpw), []byte(app.Passwd)) == 0 {
            c.String(200, "access denied")
            return
        }
        stats := make(map[string]interface{})
        stats["numFeeds"] = len(store.FeedsAll())
        stats["numPosts"] = len(store.PostsFilter(-1, func (*Post) bool { return true; }))
        c.HTML(200, "control.tmpl", gin.H{"stats": stats})
    });

    r.POST(url("/c/"), func(c *gin.Context) {
        userpw := c.Query("passwd")
        if subtle.ConstantTimeCompare([]byte(userpw), []byte(app.Passwd)) == 0 {
            c.String(200, "access denied")
            return
        }
        key := c.PostForm("cmd")
        val := c.PostForm("arg")
        c.String(200, key + " = " + val)
    })
    */

    /*   /x/*- APIs */

    r.GET(url("/x/r/:articleid"), func(c *gin.Context) {
        postID := UnhashID(c.Param("articleid"))
        if err != nil { c.String(200, err.Error()); return }
        post := store.PostsGet(postID)
        if post == nil { c.String(200, fmt.Sprintf("invalid article: %d", postID)); return }
        r, err := store.ReadabilityGetOne(post.ID)
        if err != nil { c.String(200, err.Error()); return }
        js, err := json.Marshal(r)
        if err != nil { c.String(200, err.Error()); return }
        c.String(200, string(js))
    });

    r.Run(*serveBindAddress)

    return nil
}

func cmdUpdateDb() error {
    store, err := NewStore(*appDbPath, NewPrefixedLogger("store"))
    if err != nil {
        return err
    }
    defer store.Close()

    return store.UpdateDB()
}

func cmdAddFeed(handle string, address string) error {
	if !handleRE.MatchString(handle) {
		return errors.New("invalid handle")
	}

    store, err := NewStore(*appDbPath, NewPrefixedLogger("store"))
    if err != nil {
        return err
    }
    defer store.Close()

	feed := &Feed{
		ID: MakeID(),
		URL: address,
		Handle: handle,
	}
	if store.FeedsExists(feed) {
		return errors.New("feed exists")
	} else {
		return store.FeedsSet(feed)
	}
}

func cmdDeleteFeed(handleOrAddress string) error {
	return errors.New("not implemented")
}

func cmdListFeeds() error {
    store, err := NewStore(*appDbPath, NewPrefixedLogger("store"))
    if err != nil {
        return err
    }
    defer store.Close()

	feeds := store.FeedsAll()
	for i, feed := range(feeds) {
		fmt.Printf("%d\n", i)
		fmt.Printf("  %s | %s\n", feed.Handle, feed.Title)
		fmt.Printf("  %s\n", feed.URL)
	}
	return nil
}

func main() {
    kingpin.Version("0.1.1")

    var funclet func()error = nil
    HashIDInit()
    logger = NewPrefixedLogger("main")

    switch kingpin.MustParse(app.Parse(os.Args[1:])) {
    case "serve":
        funclet = cmdRun
    case "add feed":
        funclet = func()error { return cmdAddFeed(*addFeedHandle, *addFeedAddress) }
    case "add request":
        funclet = func()error { return errors.New("not implemented") }
    case "delete feed":
        funclet = func()error { return cmdDeleteFeed(*delFeedHandleOrAddress) }
	case "clear requests":
		funclet = func()error { return errors.New("not implemented") }
    case "list feeds":
        funclet = func()error { return cmdListFeeds() }
    case "init":
        funclet = func()error { return cmdInit(*appDbPath) }
    case "init empty":
        funclet = func()error { return cmdInitDefaults(*appDbPath) }
    case "migrate":
        funclet = cmdUpdateDb
    default:
        kingpin.Usage()
    }
    if err := funclet(); err != nil {
        logger.Fatal(err.Error())
    }
}
