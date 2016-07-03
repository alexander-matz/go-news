package main;

import (
    "time"
    "log"
    "flag"
    "os"
    "sort"
    "fmt"
    "strings"
    "html/template"
    "encoding/json"
    "errors"
    "crypto/subtle"
    _ "net/url"

    "github.com/gin-gonic/gin"
    )


const perPage int = 25
const dbFile string = "./data.bolt"
const configFile string = "./config.json"

var logger *log.Logger

func loadHTMLGlob(engine *gin.Engine, pattern string) {
    funcMap := template.FuncMap{
        "hashID": HashID,
        "date": func (t time.Time) string {
            return t.Format("2006-01-02 15:04 -0700")
        },
        "when": func (t time.Time) string {
            return DurationToHuman(t.UTC().Sub(time.Now().UTC()))
        },
        "lastPost": func (posts []*Post) *Post {
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

type config struct {
    BaseURL string  `json:"baseUrl,omitempty"`
    Addr    string  `json:"address,omitempty"`
    Passwd  string  `json:"passwd,omitempty"`
}

func loadConfig(filename string) (*config, error) {
    file, err := os.Open(filename)
    if err != nil {
        return nil, err
    }
    defer file.Close()
    decoder := json.NewDecoder(file)
    res := &config{"", ":8080", ""}
    err = decoder.Decode(res)
    if err != nil {
        return nil, err
    }
    return res, nil
}

func cmdRun() error {

    var err error

    config, err := loadConfig(configFile)
    if err != nil {
        return err;
    }
    if config.Passwd == "" {
        return errors.New("NO PASSWORD SPECIFIED")
    }

    baseURL := config.BaseURL
    addr := config.Addr
    passwd := config.Passwd

    /* START SUBSYSTEMS */

    store, err := NewStore(dbFile, NewPrefixedLogger("store"))
    if err != nil {
        return err;
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

    /* CONFIGURE SERVER */
    r := gin.Default()

    loadHTMLGlob(r, "./templates/*")

    //AddPprof(r)

    r.Static(baseURL + "/static", "./static")

    /*   /   - INDEX */

    r.GET(baseURL + "/", func(c *gin.Context) {
        sitemap := make(map[string]string)
        sitemap["/f/"] = "show all feeds"
        sitemap["/f/bbc+wik"] = "show only feeds BBC and Wiki News"
        sitemap["/l/"] = "list available feeds"
        sitemap["/r/"] = "request a feed to be added"
        //sitemap["/i/"] = "statistics"
        c.HTML(200, "index.tmpl", gin.H{"base": baseURL, "sitemap": sitemap})
    })


    /*   /f/ - NEWS */

    r.GET(baseURL + "/f/", func(c *gin.Context) {
        after := c.Query("after")
        path := c.Request.URL.Path
        var posts []*Post
        var feedsMap map[int64]*Feed
        if after == "" {
            posts = store.PostsAll(perPage)
            feedsMap = store.FeedsAllMap()
        } else {
            id := UnhashID(after)
            p := store.PostsID(id)
            if p == nil {
                c.String(200, "Nothing found")
                return
            }
            posts = store.PostsAllAfter(perPage, p.Date)
            feedsMap = store.FeedsAllMap()
        }
        c.HTML(200, "posts.tmpl", gin.H{"posts": posts, "feeds": feedsMap,
                                        "base": baseURL, "path": path})
    })
    r.GET(baseURL + "/f/:feeds", func(c *gin.Context) {
        after := c.Query("after")
        feedlist := strings.Split(c.Param("feeds"), "+")
        path := c.Request.URL.Path
        var posts []*Post
        var feedsMap map[int64]*Feed
        if after == "" {
            posts = store.PostsByFeeds(perPage, feedlist)
            log.Printf("found %d posts", len(posts))
            feedsMap = store.FeedsAllMap()
        } else {
            id := UnhashID(after)
            p := store.PostsID(id)
            if p == nil {
                c.String(200, "Nothing found")
                return
            }
            posts = store.PostsByFeedsAfter(perPage, feedlist, p.Date)
            feedsMap = store.FeedsAllMap()
        }
        c.HTML(200, "posts.tmpl", gin.H{"posts": posts, "feeds": feedsMap,
                                        "base": baseURL, "path": path})
    })

    /*   /l/ - FEED LIST */

    r.GET(baseURL + "/l/", func(c *gin.Context) {
        feeds := store.FeedsAll()
        c.HTML(200, "feeds.tmpl", gin.H{"feeds": feeds, "base": baseURL})
    })


    /*   /a/*- ARTICLES */

    r.GET(baseURL + "/a/:articleid", func(c *gin.Context) {
        postID := UnhashID(c.Param("articleid"))
        if err != nil { c.String(200, "Internal error"); return }
        post := store.PostsID(postID)
        if post == nil { c.String(200, fmt.Sprintf("invalid article: %s", HashID(postID))); return }
        feedsMap := store.FeedsAllMap()
        feed := feedsMap[post.Feed];
        r, err := store.ReadabilityGetOne(post.ID)
        if err != nil { c.String(200, err.Error()); return }
        c.HTML(200, "article.tmpl", gin.H{"post": post, "content": template.HTML(r.Content),
                                            "feed": feed, "base": baseURL})
    })

    /*   /r/ - FEED REQUESTS */

    r.GET(baseURL + "/r/", func (c *gin.Context) {
        requests, err := store.FeedReqsAll()
        if err != nil { c.String(200, "Internal error"); return }
        _ = sort.IsSorted(FeedReqsByCount(requests))
        sort.Reverse(FeedReqsByCount(requests))
        c.HTML(200, "requests.tmpl", gin.H{"requests": requests, "base": baseURL})
    })
    r.POST(baseURL + "/r/", func (c *gin.Context) {
        reqURL := strings.Trim(c.PostForm("feedurl"), " \t\n\r\f")
        if ! ValidateURL(reqURL) {
            c.String(200, "malformed feed request url")
            return
        }
        store.FeedReqsAdd(reqURL)
        if err != nil { c.String(200, "Internal error"); return }
        c.Redirect(303, baseURL + "/r/")
    })

    /*   /c/*- CONTROL CENTER */

    r.GET(baseURL + "/c/", func(c *gin.Context) {
        userpw := c.Param("passwd")
        if subtle.ConstantTimeCompare([]byte(userpw), []byte(passwd)) == 1 {
            c.String(200, "access denied")
            return
        }
        stats := make(map[string]interface{})
        stats["numFeeds"] = len(store.FeedsAll())
        stats["numPosts"] = len(store.PostsAll(-1))
        c.HTML(200, "control.tmpl", gin.H{"stats": stats})
    });

    r.POST(baseURL + "/c/", func(c *gin.Context) {
        userpw := c.Param("passwd")
        if subtle.ConstantTimeCompare([]byte(userpw), []byte(passwd)) == 1 {
            c.String(200, "access denied")
            return
        }
        key := c.PostForm("cmd")
        val := c.PostForm("arg")
        c.String(200, key + " = " + val)
    })

    /*   /x/*- APIs */

    r.GET(baseURL + "/x/r/:articleid", func(c *gin.Context) {
        postID := UnhashID(c.Param("articleid"))
        if err != nil { c.String(200, err.Error()); return }
        post := store.PostsID(postID)
        if post == nil { c.String(200, fmt.Sprintf("invalid article: %d", postID)); return }
        r, err := store.ReadabilityGetOne(post.ID)
        if err != nil { c.String(200, err.Error()); return }
        js, err := json.Marshal(r)
        if err != nil { c.String(200, err.Error()); return }
        c.String(200, string(js))
    });

    r.Run(addr)

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
    name    string
    desc    string
    fun     func () error
}
var commands []*command

func help() error {
    log.Printf("Usage:")
    log.Printf("    go-news command")
    log.Printf("")
    log.Printf("Commands are:")
    var longest int
    for _, cmd := range(commands) {
        if len(cmd.name) > longest {
            longest = len(cmd.name)
        }
    }

    for _, cmd := range(commands) {
        log.Printf("    % -*s  %s", longest, cmd.name, cmd.desc)
    }
    return nil
}

func doCommand(name string) error {
    for _, cmd := range(commands) {
        if cmd.name == name {
            return cmd.fun()
        }
    }
    return errors.New("unknown command: "+name)
}

func addCommand(fun func()error, name, desc string) {
    if commands == nil {
        commands = make([]*command, 0)
    }
    commands = append(commands, &command{name, desc, fun})
}

func main() {
    addCommand(cmdRun, "run", "start service regularly")
    addCommand(cmdInit, "init", "initialize empty database")
    addCommand(cmdHash, "hash", "generate a hash from password")
    addCommand(cmdCmd, "cmd", "send a command to a running service")

    addCommand(cmdBackup, "backup", "backup database into json (stdout)")
    addCommand(cmdRestore, "restore", "restore database from json (stdin)")

    addCommand(cmdTest, "test", "unspecified tests for development")

    addCommand(cmdUpdateDB, "updatedb", "migrate database to current format")

    if len(os.Args) < 1 {
        help()
        return
    }

    HashIDInit()
    logger = log.New(os.Stderr, "LOG| ", 0)
    ValidateURL("www.google.de")

    if err := doCommand(os.Args[1]); err != nil {
        logger.Fatal(err.Error())
    }
}
